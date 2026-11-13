package diagnose_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func restoreInput(t testing.TB) diagnose.ClusterInput {
	t.Helper()
	i := input(t)
	request := readWorkloadFixture[evidence.Restore](t, "restore")
	at := *request.Reference.CollectedAt.Value
	i.Observation.Reference.CollectedAt = timestamp(at)
	i.Collection.Interval = evidence.Interval{StartedAt: at, CompletedAt: at}
	ref := i.Observation.Reference
	later := at.Add(time.Second)
	ref.CollectedAt = timestamp(later)
	i.Verification = &evidence.TargetVerification{
		Before:           i.Observation.Reference,
		BeforeGeneration: i.Observation.Generation, After: &ref, AfterGeneration: i.Observation.Generation,
		State: evidence.VerificationVerified, Read: evidence.Collection{
			Scope: i.Collection.Scope, Outcome: evidence.CollectionComplete, Items: 1,
			Interval: evidence.Interval{StartedAt: later, CompletedAt: later},
		},
	}
	read := i.Collection
	read.Scope.Name, read.Scope.Resource, read.Scope.Action = "", "openbaorestores", evidence.ReadList
	i.Restores = &diagnose.ResourceInput[evidence.Restore]{
		Collection: read, Observations: []evidence.Restore{request},
	}
	return i
}

func restorePolicy(t testing.TB) diagnose.Policy {
	t.Helper()
	i := restoreInput(t)
	p := policy(t)
	p.ReferenceTime = i.Verification.Read.Interval.CompletedAt
	p.Rules = []diagnose.RuleID{diagnose.RuleRestore}
	return p
}

func runRestore(t *testing.T, i diagnose.ClusterInput, p diagnose.Policy) (diagnose.Report, diagnose.Coverage) {
	t.Helper()
	report, err := diagnose.Analyze(capture(i), p)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range report.Coverage {
		if entry.Rule == diagnose.RuleRestore {
			return report, entry
		}
	}
	t.Fatal("restore rule has no coverage entry")
	return diagnose.Report{}, diagnose.Coverage{}
}

func TestRestoreFindingsSeparateHeldHistoricalAndProgress(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, phase, stage, cleanup, hold string
		history                           bool
		want                              diagnose.Severity
	}{
		{"held-unknown", "Unknown", "Committed", "Complete", "operation-1", false, diagnose.Error},
		{"held-failure-by-uid", "Failed", "TerminalObserved", "Complete", "request-1", false, diagnose.Error},
		{"recorded-unknown", "Unknown", "Committed", "Complete", "", true, diagnose.Warning},
		{"historical-disabled", "Unknown", "Committed", "Complete", "", false, ""},
		{"recorded-cleanup", "Completed", "FollowThroughComplete", "Failed", "", true, diagnose.Warning},
		{"held-cleanup", "Completed", "FollowThroughComplete", "Failed", "operation-1", false, diagnose.Error},
		{"pending", "Pending", "Prepared", "Pending", "", false, diagnose.Info},
		{"validating", "Validating", "Prepared", "Pending", "", false, diagnose.Info},
		{"running", "Running", "Created", "Pending", "", false, diagnose.Info},
		{"completed", "Completed", "FollowThroughComplete", "Complete", "", true, ""},
		{"unknown-receipt-after-completion", "Completed", "Unknown", "Complete", "operation-1", false, diagnose.Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, p := restoreInput(t), restorePolicy(t)
			r := &i.Restores.Observations[0]
			r.Phase, r.Execution.Stage, r.Target.Cleanup = text(tc.phase), text(tc.stage), text(tc.cleanup)
			if tc.hold != "" {
				i.Observation.RestoreHold = text(tc.hold)
			}
			p.ReportHistoricalRestores = tc.history
			report, coverage := runRestore(t, i, p)
			if coverage.State != diagnose.Evaluated {
				t.Fatal("valid recorded restore facts lost coverage")
			}
			if tc.want == "" {
				if len(report.Findings) != 0 {
					t.Fatal("completion or unselected history became a concern")
				}
			} else if len(report.Findings) != 1 || report.Findings[0].Severity != tc.want ||
				report.Findings[0].Subject != r.Reference.Identity {
				t.Fatal("restore finding lost its subject or severity")
			}
		})
	}
}

func TestRestoreHoldNeverProvesUIDOwnership(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*diagnose.ClusterInput)
	}{
		{"name-and-time-only", func(i *diagnose.ClusterInput) {
			r := &i.Restores.Observations[0]
			r.Target.UID, r.Execution.TargetUID = evidence.Text{State: evidence.ValueUnknown}, evidence.Text{State: evidence.ValueUnknown}
		}},
		{"conflicting-target", func(i *diagnose.ClusterInput) { i.Restores.Observations[0].Target.UID = text("replacement") }},
		{"conflicting-name", func(i *diagnose.ClusterInput) { i.Restores.Observations[0].TargetName = text("other") }},
	} {
		for _, hold := range []string{"operation-1", "request-1"} {
			t.Run(tc.name+"/"+hold, func(t *testing.T) {
				i, p := restoreInput(t), restorePolicy(t)
				i.Observation.RestoreHold = text(hold)
				tc.change(&i)
				report, coverage := runRestore(t, i, p)
				if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
					t.Fatal("hold/name/time replaced independent UID confirmation")
				}
			})
		}
	}
}

func TestRestoreUnconfirmedBindingsRemainInsufficient(t *testing.T) {
	t.Parallel()
	i, p := restoreInput(t), restorePolicy(t)
	i.Observation.RestoreOrigin = evidence.Text{State: evidence.ValueUnknown}
	i.Restores.Observations[0].Target.UID = evidence.Text{State: evidence.ValueUnknown}
	i.Restores.Observations[0].Execution.TargetUID = evidence.Text{State: evidence.ValueUnknown}
	report, coverage := runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || coverage.Reason != diagnose.ReasonAssociation || len(report.Findings) != 0 {
		t.Fatal("missing UID bindings confirmed restore ownership")
	}
}

func TestRestoreMissingPhaseAndReceiptsDoNotInferCompletion(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"Prepared", "Committed", "Created", "TerminalObserved", "FollowThroughComplete"} {
		i, p := restoreInput(t), restorePolicy(t)
		r := &i.Restores.Observations[0]
		r.Phase = evidence.Text{State: evidence.ValueUnknown}
		r.Target.Cleanup, r.Execution.Stage = text("Complete"), text(stage)
		// Submission, application, restart, and administrator receipts are all
		// present in this fixture. None supplies the missing phase.
		report, coverage := runRestore(t, i, p)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
			t.Fatal("request age or independent receipt became a phase/result")
		}
	}
	i, p := restoreInput(t), restorePolicy(t)
	i.Observation.RestoreHold = text("operation-1")
	i.Restores.Observations[0].Phase = evidence.Text{State: evidence.ValueInvalid}
	i.Restores.Observations[0].Execution.Stage = text("Unknown")
	report, coverage := runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 || report.Findings[0].Severity != diagnose.Error {
		t.Fatal("invalid phase erased an independently unknown held execution")
	}
}

func TestAmbiguousRestoreHoldAndReplayRemainDeterministic(t *testing.T) {
	t.Parallel()
	i, p := restoreInput(t), restorePolicy(t)
	other := readWorkloadFixture[evidence.Restore](t, "restore")
	other.Reference.Identity.Name, other.Reference.Identity.UID = "restore-2", "request-2"
	i.Restores.Observations = append(i.Restores.Observations, other)
	i.Restores.Collection.Items = 2
	i.Observation.RestoreHold = text("operation-1")
	p.ReportHistoricalRestores = false
	report, coverage := runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
		t.Fatal("ambiguous operation ID selected a held request")
	}
	i.Observation.RestoreHold = text("request-1")
	first, coverage := runRestore(t, i, p)
	if coverage.State != diagnose.Evaluated || len(first.Findings) != 1 || first.Findings[0].Severity != diagnose.Error {
		t.Fatal("unique request UID did not bind the independently confirmed hold")
	}
	data, err := json.Marshal(capture(i))
	if err != nil {
		t.Fatal(err)
	}
	var decoded diagnose.Capture
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	slices.Reverse(decoded.Clusters[0].Restores.Observations)
	second, err := diagnose.Analyze(decoded, p)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil || !bytes.Equal(a, b) || strings.Contains(string(a), "CANARY") {
		t.Fatal("restore replay changed result order or copied source text")
	}
}

func TestRestoreReadAndTimeBoundariesRetainIndependentFindings(t *testing.T) {
	t.Parallel()
	i, p := restoreInput(t), restorePolicy(t)
	i.Observation.RestoreHold = text("operation-1")
	i.Restores.Collection.Outcome = evidence.CollectionPartial
	report, coverage := runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 {
		t.Fatal("partial restore list erased independent UID-bound concern")
	}
	i.Restores.Observations[0].CompletedAt = timestamp(p.ReferenceTime.Add(time.Second))
	report, coverage = runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 {
		t.Fatal("future producer time erased an independent reported concern")
	}
	i = restoreInput(t)
	i.Verification = nil
	report, coverage = runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
		t.Fatal("unverified target produced restore findings")
	}
	i = restoreInput(t)
	i.Verification.AfterGeneration, i.Verification.State = integer(5), evidence.VerificationChanged
	report, coverage = runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
		t.Fatal("changed target produced restore findings")
	}
	i = restoreInput(t)
	i.Restores.Observations, i.Restores.Collection.Items, i.Restores.Collection.Outcome = []evidence.Restore{}, 0, evidence.CollectionDenied
	i.Observation.RestoreHold = text("operation-1")
	report, coverage = runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
		t.Fatal("denied restore read became a resolved hold")
	}
}

func TestRestoreProducerTimesUseTheRequestRead(t *testing.T) {
	t.Parallel()
	i, p := restoreInput(t), restorePolicy(t)
	at := *i.Restores.Observations[0].Reference.CollectedAt.Value
	earlier := at.Add(-30 * time.Second)
	i.Observation.Reference.CollectedAt = timestamp(earlier)
	i.Collection.Interval = evidence.Interval{StartedAt: earlier, CompletedAt: earlier}
	i.Verification.Before = i.Observation.Reference
	r := &i.Restores.Observations[0]
	r.StartedAt, r.CompletedAt = timestamp(at.Add(-10*time.Second)), timestamp(at.Add(-5*time.Second))
	r.Target.AppliedAt, r.RestartCompletedAt = timestamp(at.Add(-8*time.Second)), timestamp(at.Add(-3*time.Second))
	report, coverage := runRestore(t, i, p)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 {
		t.Fatal("valid producer times were compared with the earlier cluster read")
	}
	r.CompletedAt = timestamp(at.Add(-20 * time.Second))
	report, coverage = runRestore(t, i, p)
	if coverage.State != diagnose.Insufficient || coverage.Reason != diagnose.ReasonTime || len(report.Findings) != 1 {
		t.Fatal("reversed producer times hid independent reported facts")
	}
}

func TestRestoreInputRejectsMismatchedVersionsAndSubjects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*diagnose.ClusterInput, *diagnose.Policy)
		want   error
	}{
		{"version", func(i *diagnose.ClusterInput, _ *diagnose.Policy) {
			i.Restores.Observations[0].Reference.APIVersion = "openbao.org/v1alpha2"
		}, diagnose.ErrInvalidCapture},
		{
			"invalid-reference-time", func(_ *diagnose.ClusterInput, p *diagnose.Policy) { p.ReferenceTime = time.Time{} },
			diagnose.ErrInvalidPolicy,
		},
		{"subject-budget", func(_ *diagnose.ClusterInput, p *diagnose.Policy) { p.MaxSubjects = 1 }, diagnose.ErrInvalidCapture},
		{"duplicate-request", func(i *diagnose.ClusterInput, _ *diagnose.Policy) {
			i.Restores.Observations = append(i.Restores.Observations, i.Restores.Observations[0])
			i.Restores.Collection.Items = 2
		}, diagnose.ErrInvalidCapture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, p := restoreInput(t), restorePolicy(t)
			tc.change(&i, &p)
			if _, err := diagnose.Analyze(capture(i), p); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
