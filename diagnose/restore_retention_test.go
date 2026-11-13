package diagnose_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func retentionInput(t testing.TB, native bool) (diagnose.ClusterInput, diagnose.Policy) {
	t.Helper()
	i, p := restoreGroupInput(t, native)
	r := &i.Restores.Observations[0]
	r.TargetLifecycle, r.CleanupAfterSeconds = text("Disposable"), integer(3600)
	r.Target.AppliedAt = timestamp(r.Reference.CollectedAt.Value.Add(-30 * time.Minute))
	r.Target.Cleanup = evidence.Text{State: evidence.ValueUnknown}
	r.Execution.Stage, r.Execution.TerminalResult = text("Unknown"), text("Succeeded")
	r.CompletedAt, r.RestartCompletedAt = evidence.Timestamp{State: evidence.ValueUnknown}, evidence.Timestamp{State: evidence.ValueUnknown}
	r.Restart, r.AdministratorDisposition = evidence.ObjectAbsent, evidence.Text{State: evidence.ValueUnknown}
	i.Observation.RestoreHold = text(r.Reference.Identity.UID)
	p.ReportHistoricalRestores = false
	return i, p
}

func assertRetentionReferences(t *testing.T, finding diagnose.Finding) {
	t.Helper()
	for _, field := range []string{
		"spec.targetLifecycle", "spec.cleanupAfterSeconds", "status.target.appliedAt", "status.execution.terminalResult",
	} {
		found := false
		for _, ref := range finding.References {
			found = found || ref.Field == field
		}
		if !found {
			t.Fatal("retention finding omitted a required evidence reference")
		}
	}
}

func assertRetentionReplay(t *testing.T, encoded []byte, policy diagnose.Policy, report diagnose.Report) {
	t.Helper()
	var replay diagnose.Capture
	if err := json.Unmarshal(encoded, &replay); err != nil {
		t.Fatal(err)
	}
	second, err := diagnose.Analyze(replay, policy)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("serialized retention replay changed the report")
	}
}

func TestDisposableRetentionReportsApplicationWithoutCompletingRestore(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{true, false} {
		i, p := retentionInput(t, native)
		t.Run(i.Subject.Group, func(t *testing.T) {
			before, err := json.Marshal(capture(i))
			if err != nil {
				t.Fatal(err)
			}
			report, coverage := runRestore(t, i, p)
			if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 || report.Findings[0].Severity != diagnose.Info ||
				!strings.Contains(report.Findings[0].Summary, "operator reports snapshot application") ||
				!strings.Contains(report.Findings[0].Summary, "cleanup is pending") {
				t.Fatal("confirmed disposable retention did not report its bounded waiting state")
			}
			after, err := json.Marshal(capture(i))
			if err != nil || !bytes.Equal(before, after) || *i.Restores.Observations[0].Phase.Value != "Unknown" {
				t.Fatal("retention interpretation mutated phase or normalized receipts")
			}
			assertRetentionReferences(t, report.Findings[0])
			assertRetentionReplay(t, before, p, report)
			p.ReferenceTime = i.Restores.Observations[0].Target.AppliedAt.Value.Add(time.Hour)
			p.MaxEvidenceAge = time.Hour
			expired, _ := runRestore(t, i, p)
			if len(expired.Findings) != 1 || expired.Findings[0].Severity != diagnose.Error {
				t.Fatal("offline reference time extended an expired cleanup retention window")
			}
		})
	}
}

func TestDisposableRetentionRejectsContradictoryStages(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{true, false} {
		for _, stage := range []string{"Prepared", "Committed", "Created", "TerminalObserved", "FollowThroughComplete"} {
			i, p := retentionInput(t, native)
			t.Run(i.Subject.Group+"/"+stage, func(t *testing.T) {
				i.Restores.Observations[0].Execution.Stage = text(stage)
				report, _ := runRestore(t, i, p)
				if len(report.Findings) != 1 || report.Findings[0].Severity != diagnose.Error {
					t.Fatal("contradictory execution stage hid the held Unknown concern")
				}
			})
		}
	}
}

func TestFailedTerminalReceiptRemainsAdverseAfterReportedCompletion(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{true, false} {
		for _, held := range []bool{true, false} {
			i, p := retentionInput(t, native)
			r := &i.Restores.Observations[0]
			r.Phase, r.Execution.Stage = text("Completed"), text("FollowThroughComplete")
			r.Execution.TerminalResult = text("Failed")
			want := diagnose.Error
			if !held {
				i.Observation.RestoreHold = evidence.Text{State: evidence.ValueUnknown}
				p.ReportHistoricalRestores = true
				want = diagnose.Warning
			}
			report, _ := runRestore(t, i, p)
			if len(report.Findings) != 1 || report.Findings[0].Severity != want {
				t.Fatal("reported completion hid the contradictory failed terminal receipt")
			}
		}
	}
}

func TestDisposableRetentionNeverHidesIncompleteOrConflictingReceipts(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{true, false} {
		for _, tc := range []struct {
			name   string
			change func(*diagnose.ClusterInput)
		}{
			{"retained-target", func(i *diagnose.ClusterInput) { i.Restores.Observations[0].TargetLifecycle = text("Retain") }},
			{"missing-lifecycle", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].TargetLifecycle = evidence.Text{State: evidence.ValueUnknown}
			}},
			{"invalid-lifecycle", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].TargetLifecycle = evidence.Text{State: evidence.ValueInvalid}
			}},
			{"missing-retention", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].CleanupAfterSeconds = evidence.Integer{State: evidence.ValueUnknown}
			}},
			{"invalid-retention", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].CleanupAfterSeconds = evidence.Integer{State: evidence.ValueInvalid}
			}},
			{"immediate-cleanup", func(i *diagnose.ClusterInput) { i.Restores.Observations[0].CleanupAfterSeconds = integer(0) }},
			{"expired-retention", func(i *diagnose.ClusterInput) {
				r := &i.Restores.Observations[0]
				r.Target.AppliedAt = timestamp(r.Reference.CollectedAt.Value.Add(-time.Hour))
			}},
			{"missing-application", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Target.AppliedAt = evidence.Timestamp{State: evidence.ValueUnknown}
			}},
			{"invalid-application", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Target.AppliedAt = evidence.Timestamp{State: evidence.ValueInvalid}
			}},
			{"future-application", func(i *diagnose.ClusterInput) {
				r := &i.Restores.Observations[0]
				r.Target.AppliedAt = timestamp(r.Reference.CollectedAt.Value.Add(time.Second))
			}},
			{"application-before-request", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Target.AppliedAt = timestamp(i.Restores.Observations[0].CreatedAt.Value.Add(-time.Second))
			}},
			{"missing-target-uid", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Target.UID = evidence.Text{State: evidence.ValueUnknown}
			}},
			{"missing-terminal-result", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Execution.TerminalResult = evidence.Text{State: evidence.ValueUnknown}
			}},
			{"invalid-terminal-result", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Execution.TerminalResult = evidence.Text{State: evidence.ValueInvalid}
			}},
			{"failed-terminal-result", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Execution.TerminalResult = text("Failed")
			}},
			{"failed-phase", func(i *diagnose.ClusterInput) { i.Restores.Observations[0].Phase = text("Failed") }},
			{"failed-cleanup", func(i *diagnose.ClusterInput) { i.Restores.Observations[0].Target.Cleanup = text("Failed") }},
			{"invalid-cleanup", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Target.Cleanup = evidence.Text{State: evidence.ValueInvalid}
			}},
			{"completed-cleanup-with-held-target", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].Target.Cleanup = text("Complete")
			}},
			{"administrator-disposition", func(i *diagnose.ClusterInput) {
				i.Restores.Observations[0].AdministratorDisposition = text("Abandon")
			}},
			{"managed-restart", func(i *diagnose.ClusterInput) { i.Restores.Observations[0].Restart = evidence.ObjectReported }},
			{"partial-list", func(i *diagnose.ClusterInput) { i.Restores.Collection.Outcome = evidence.CollectionPartial }},
			{"restricted-list", func(i *diagnose.ClusterInput) { i.Restores.Collection.Scope.Selector = "app=bao" }},
		} {
			i, p := retentionInput(t, native)
			t.Run(i.Subject.Group+"/"+tc.name, func(t *testing.T) {
				tc.change(&i)
				report, _ := runRestore(t, i, p)
				if len(report.Findings) != 1 || report.Findings[0].Severity != diagnose.Error {
					t.Fatal("incomplete, failed, or contradictory retention facts hid a held Unknown concern")
				}
			})
		}
	}
}

func TestDisposableRetentionStillRequiresUniqueHoldAndCurrentUID(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{true, false} {
		i, p := retentionInput(t, native)
		i.Observation.RestoreHold = evidence.Text{State: evidence.ValueUnknown}
		report, _ := runRestore(t, i, p)
		if len(report.Findings) != 0 {
			t.Fatal("an unheld historical request became a current cleanup waiting state")
		}
		i, p = retentionInput(t, native)
		r := &i.Restores.Observations[0]
		r.Target.UID, r.Execution.TargetUID = evidence.Text{State: evidence.ValueUnknown}, evidence.Text{State: evidence.ValueUnknown}
		i.Observation.RestoreOrigin = evidence.Text{State: evidence.ValueUnknown}
		report, coverage := runRestore(t, i, p)
		if len(report.Findings) != 0 || coverage.State != diagnose.Insufficient {
			t.Fatal("retention fields or a hold supplied missing target UID confirmation")
		}
		i, p = retentionInput(t, native)
		other := i.Restores.Observations[0]
		other.Reference.Identity.Name, other.Reference.Identity.UID = "other", "request-other"
		i.Restores.Observations = append(i.Restores.Observations, other)
		i.Restores.Collection.Items = 2
		i.Observation.RestoreHold = text(*other.Execution.OperationID.Value)
		report, coverage = runRestore(t, i, p)
		if len(report.Findings) != 0 || coverage.State != diagnose.Insufficient {
			t.Fatal("ambiguous operation hold selected a cleanup retention request")
		}
	}
}

func TestRestoreRetentionRejectsMalformedNormalizedEvidence(t *testing.T) {
	t.Parallel()
	i, p := retentionInput(t, true)
	i.Restores.Observations[0].CleanupAfterSeconds = evidence.Integer{}
	if _, err := diagnose.Analyze(capture(i), p); !errors.Is(err, diagnose.ErrInvalidCapture) {
		t.Fatal("missing value state bypassed typed validation")
	}
}

func TestRestoreRetentionRejectsUnboundedOrImpliedNormalizedSeconds(t *testing.T) {
	t.Parallel()
	for _, value := range []evidence.Integer{
		integer(-1), integer(86401), {State: evidence.ValueImplied, Value: new(int64)},
	} {
		i, p := retentionInput(t, true)
		i.Restores.Observations[0].CleanupAfterSeconds = value
		if _, err := diagnose.Analyze(capture(i), p); !errors.Is(err, diagnose.ErrInvalidCapture) {
			t.Fatal("normalized retention accepted an out-of-range or inferred cleanup period")
		}
	}
}
