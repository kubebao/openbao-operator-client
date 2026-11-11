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

func TestBackupPrecedenceRequiresObservedFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*evidence.Backup)
		want   diagnose.RuleID
		state  diagnose.CoverageState
	}{
		{"overdue", func(_ *evidence.Backup) {}, diagnose.RuleBackupAge, diagnose.Evaluated},
		{
			"failure-before-age", func(b *evidence.Backup) { b.ConsecutiveFailures = integer(2) },
			diagnose.RuleBackupFailed, diagnose.Evaluated,
		},
		{"failure-before-missing-success", func(b *evidence.Backup) {
			b.ConsecutiveFailures = integer(1)
			b.LastSuccessAt = evidence.Timestamp{State: evidence.ValueUnknown}
		}, diagnose.RuleBackupFailed, diagnose.Evaluated},
		{"no-recorded-success", func(b *evidence.Backup) {
			b.LastSuccessAt = evidence.Timestamp{State: evidence.ValueUnknown}
		}, diagnose.RuleBackupAbsent, diagnose.Evaluated},
		{"missing-count", func(b *evidence.Backup) {
			b.ConsecutiveFailures = evidence.Integer{State: evidence.ValueUnknown}
		}, "", diagnose.Insufficient},
		{"invalid-count", func(b *evidence.Backup) {
			b.ConsecutiveFailures = evidence.Integer{State: evidence.ValueInvalid}
		}, "", diagnose.Insufficient},
		{"missing-count-and-success", func(b *evidence.Backup) {
			b.ConsecutiveFailures = evidence.Integer{State: evidence.ValueUnknown}
			b.LastSuccessAt = evidence.Timestamp{State: evidence.ValueUnknown}
		}, "", diagnose.Insufficient},
		{"unconfigured", func(b *evidence.Backup) { b.Configuration = evidence.ObjectAbsent }, "", diagnose.NotApplicable},
		{"unknown-configuration", func(b *evidence.Backup) { b.Configuration = evidence.ObjectUnknown }, "", diagnose.Insufficient},
		{"null-configuration", func(b *evidence.Backup) { b.Configuration = evidence.ObjectNull }, "", diagnose.Insufficient},
		{"unreported-status", func(b *evidence.Backup) {
			b.State = evidence.ObjectAbsent
			b.LastSuccessAt = evidence.Timestamp{State: evidence.ValueUnknown}
			b.ConsecutiveFailures = evidence.Integer{State: evidence.ValueUnknown}
			b.RestoreTest.State = evidence.ObjectUnknown
		}, "", diagnose.Insufficient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := input(t)
			tc.change(&i.Observation.Backup)
			p := policy(t)
			p.Rules = []diagnose.RuleID{diagnose.RuleBackupFailed, diagnose.RuleBackupAbsent, diagnose.RuleBackupAge}
			report, err := diagnose.Analyze(capture(i), p)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(report.Findings) != 0 {
					t.Fatal("unknown or invalid evidence became a backup finding")
				}
			} else if len(report.Findings) != 1 || report.Findings[0].Rule != tc.want || report.Findings[0].Severity != diagnose.Warning {
				t.Fatal("backup precedence or severity changed")
			}
			for _, rule := range []diagnose.RuleID{diagnose.RuleBackupFailed, diagnose.RuleBackupAbsent, diagnose.RuleBackupAge} {
				if tc.want != "" && rule != tc.want {
					continue
				}
				_, coverage := runRule(t, i, rule)
				if coverage.State != tc.state {
					t.Fatalf("%s coverage = %s, want %s", rule, coverage.State, tc.state)
				}
			}
		})
	}
}

func TestBackupFailureTimeAndRuleSelectionRemainSeparate(t *testing.T) {
	t.Parallel()
	i := input(t)
	i.Observation.Backup.ConsecutiveFailures = integer(1)
	i.Observation.Backup.LastFailureAt = timestamp(policy(t).ReferenceTime.Add(time.Second))
	report, coverage := runRule(t, i, diagnose.RuleBackupFailed)
	if coverage.State != diagnose.Insufficient || coverage.Reason != diagnose.ReasonTime || len(report.Findings) != 1 {
		t.Fatal("inconsistent failure time erased the independently reported counter")
	}
	for _, rule := range []diagnose.RuleID{diagnose.RuleBackupAbsent, diagnose.RuleBackupAge} {
		report, coverage = runRule(t, i, rule)
		if len(report.Findings) != 0 || coverage.Reason != diagnose.ReasonHigherPriority {
			t.Fatal("excluding the failure rule enabled a lower-priority backup finding")
		}
	}
	i.Observation.Backup.ConsecutiveFailures = integer(0)
	i.Observation.Backup.LastSuccessAt = timestamp(policy(t).ReferenceTime.Add(time.Second))
	for _, rule := range []diagnose.RuleID{diagnose.RuleBackupAbsent, diagnose.RuleBackupAge} {
		report, coverage = runRule(t, i, rule)
		if len(report.Findings) != 0 || coverage.State != diagnose.Insufficient || coverage.Reason != diagnose.ReasonTime {
			t.Fatal("future success became missing or overdue")
		}
		i.Observation.Backup.LastSuccessAt = evidence.Timestamp{State: evidence.ValueInvalid}
		report, coverage = runRule(t, i, rule)
		if len(report.Findings) != 0 || coverage.State != diagnose.Insufficient {
			t.Fatal("invalid success became missing or overdue")
		}
		i.Observation.Backup.LastSuccessAt = timestamp(policy(t).ReferenceTime.Add(time.Second))
	}
}

func upgrade(t testing.TB) diagnose.ClusterInput {
	t.Helper()
	i := input(t)
	i.Observation.Upgrade.State = evidence.ObjectReported
	i.Observation.Upgrade.Failure = evidence.ControllerError{
		State: evidence.ObjectReported, Reason: text("UPGRADE_REASON_CANARY"), At: evidence.Timestamp{State: evidence.ValueUnknown},
	}
	return i
}

func TestUpgradeFailurePreservesRecordedFacts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		change   func(*evidence.Upgrade)
		state    diagnose.CoverageState
		findings int
	}{
		{"recorded-without-time", func(_ *evidence.Upgrade) {}, diagnose.Evaluated, 1},
		{
			"missing-reason", func(u *evidence.Upgrade) { u.Failure.Reason = evidence.Text{State: evidence.ValueUnknown} },
			diagnose.Insufficient, 0,
		},
		{
			"invalid-reason", func(u *evidence.Upgrade) { u.Failure.Reason = evidence.Text{State: evidence.ValueInvalid} },
			diagnose.Insufficient, 0,
		},
		{
			"future-time", func(u *evidence.Upgrade) { u.Failure.At = timestamp(policy(t).ReferenceTime.Add(time.Second)) },
			diagnose.Insufficient, 1,
		},
		{
			"invalid-time", func(u *evidence.Upgrade) { u.Failure.At = evidence.Timestamp{State: evidence.ValueInvalid} },
			diagnose.Insufficient, 1,
		},
		{"no-failure-record", func(u *evidence.Upgrade) {
			u.Failure.State, u.Failure.Reason = evidence.ObjectAbsent, evidence.Text{State: evidence.ValueUnknown}
		}, diagnose.Evaluated, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := upgrade(t)
			tc.change(&i.Observation.Upgrade)
			report, coverage := runRule(t, i, diagnose.RuleUpgrade)
			if coverage.State != tc.state || len(report.Findings) != tc.findings {
				t.Fatal("recorded upgrade failure or coverage changed")
			}
			if len(report.Findings) > 0 && report.Findings[0].Severity != diagnose.Warning {
				t.Fatal("upgrade failure severity changed")
			}
		})
	}
	i := input(t)
	i.Observation.BlueGreen.State = evidence.ObjectReported
	i.Observation.BlueGreen.JobFailureCount = integer(3)
	i.Observation.BlueGreen.Phase = text("RollingBack")
	report, coverage := runRule(t, i, diagnose.RuleUpgrade)
	if len(report.Findings) != 0 || coverage.State != diagnose.Evaluated {
		t.Fatal("blue/green observations became a rolling-upgrade failure")
	}
}

func restoreTestInput(t testing.TB) diagnose.ClusterInput {
	t.Helper()
	i := input(t)
	test := &i.Observation.Backup.RestoreTest
	test.State, test.Active, test.Last = evidence.ObjectReported, evidence.ObjectAbsent, evidence.ObjectReported
	test.LastOutcome, test.LastReason = text("Failed"), text("RESULT_REASON_CANARY")
	test.FinishedAt = timestamp(policy(t).ReferenceTime.Add(-time.Hour))
	check := condition("Passed", evidence.ConditionFalse, 4)
	check.Reason = text("Failed")
	test.Conditions = evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{check}}
	return i
}

func TestRestoreTestSeparatesActiveLastAndCondition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		change   func(*evidence.RestoreTest)
		state    diagnose.CoverageState
		findings int
	}{
		{"failed-result", func(_ *evidence.RestoreTest) {}, diagnose.Evaluated, 1},
		{"passed-result", func(r *evidence.RestoreTest) {
			r.LastOutcome = text("Passed")
			r.Conditions.Values[0].Status = evidence.ConditionTrue
		}, diagnose.Evaluated, 0},
		{"active-suppresses-old-result", func(r *evidence.RestoreTest) {
			r.Active = evidence.ObjectReported
			r.Conditions.Values[0].Status = evidence.ConditionUnknown
			r.Conditions.Values[0].Reason = text("Running")
		}, diagnose.Insufficient, 0},
		{"blocked-before-active", func(r *evidence.RestoreTest) {
			r.Active = evidence.ObjectReported
			r.Conditions.Values[0].Reason = text("BLOCK_REASON_CANARY")
		}, diagnose.Evaluated, 1},
		{"unknown-active", func(r *evidence.RestoreTest) { r.Active = evidence.ObjectUnknown }, diagnose.Insufficient, 0},
		{"invalid-active", func(r *evidence.RestoreTest) { r.Active = evidence.ObjectInvalid }, diagnose.Insufficient, 0},
		{"missing-condition-keeps-recorded-failure", func(r *evidence.RestoreTest) {
			r.Conditions = evidence.Conditions{State: evidence.ValueUnknown}
		}, diagnose.Insufficient, 1},
		{"stale-condition-keeps-recorded-failure", func(r *evidence.RestoreTest) {
			r.Conditions.Values[0].ObservedGeneration = integer(3)
			r.Conditions.Values[0].Reason = text("BLOCK_REASON_CANARY")
		}, diagnose.Insufficient, 1},
		{"missing-result-time", func(r *evidence.RestoreTest) {
			r.FinishedAt = evidence.Timestamp{State: evidence.ValueUnknown}
		}, diagnose.Insufficient, 1},
		{"future-result-time", func(r *evidence.RestoreTest) {
			r.FinishedAt = timestamp(policy(t).ReferenceTime.Add(time.Second))
		}, diagnose.Insufficient, 1},
		{"invalid-result-time", func(r *evidence.RestoreTest) {
			r.FinishedAt = evidence.Timestamp{State: evidence.ValueInvalid}
		}, diagnose.Insufficient, 1},
		{"blocked-future-transition", func(r *evidence.RestoreTest) {
			r.Conditions.Values[0].Reason = text("Blocked")
			r.Conditions.Values[0].LastTransitionAt = timestamp(policy(t).ReferenceTime.Add(time.Second))
		}, diagnose.Insufficient, 1},
		{"missing-block-reason-keeps-result", func(r *evidence.RestoreTest) {
			r.Conditions.Values[0].Reason = evidence.Text{State: evidence.ValueUnknown}
		}, diagnose.Insufficient, 1},
		{
			"invalid-outcome", func(r *evidence.RestoreTest) { r.LastOutcome = evidence.Text{State: evidence.ValueInvalid} },
			diagnose.Insufficient, 0,
		},
		{"unknown-enum", func(r *evidence.RestoreTest) { r.LastOutcome = text("FutureOutcome") }, diagnose.Insufficient, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := restoreTestInput(t)
			tc.change(&i.Observation.Backup.RestoreTest)
			report, coverage := runRule(t, i, diagnose.RuleRestoreTest)
			if coverage.State != tc.state || len(report.Findings) != tc.findings {
				t.Fatal("restore-test precedence or evidence coverage changed")
			}
			if len(report.Findings) > 0 && report.Findings[0].Severity != diagnose.Warning {
				t.Fatal("restore-test severity changed")
			}
		})
	}
}

func TestRestoreTestConditionRequiresOwnCurrentGeneration(t *testing.T) {
	t.Parallel()
	for _, ack := range []int64{0, 3, 5, -1} {
		i := restoreTestInput(t)
		i.Observation.Backup.RestoreTest.Active = evidence.ObjectReported
		check := &i.Observation.Backup.RestoreTest.Conditions.Values[0]
		check.Reason, check.ObservedGeneration = text("Blocked"), integer(ack)
		report, coverage := runRule(t, i, diagnose.RuleRestoreTest)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
			t.Fatal("noncurrent restore-test condition became a finding")
		}
	}
	i := restoreTestInput(t)
	i.Observation.ObservedGeneration = integer(3)
	i.Observation.Backup.RestoreTest.Conditions.Values[0].Reason = text("Blocked")
	report, coverage := runRule(t, i, diagnose.RuleRestoreTest)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 {
		t.Fatal("resource acknowledgement replaced the restore-test condition acknowledgement")
	}
}

func TestOperationalReplayExcludesProviderText(t *testing.T) {
	t.Parallel()
	i := restoreTestInput(t)
	i.Observation.Upgrade = upgrade(t).Observation.Upgrade
	i.Observation.Backup.ConsecutiveFailures = integer(2)
	i.Observation.Backup.LastFailureReason = text("BACKUP_REASON_CANARY")
	p := policy(t)
	p.Rules = []diagnose.RuleID{diagnose.RuleBackupFailed, diagnose.RuleBackupAbsent, diagnose.RuleUpgrade, diagnose.RuleRestoreTest}
	data, err := json.Marshal(capture(i))
	if err != nil {
		t.Fatal(err)
	}
	var decoded diagnose.Capture
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	first, err := diagnose.Analyze(decoded, p)
	if err != nil || len(first.Findings) != 3 {
		t.Fatalf("operational replay: %v", err)
	}
	slices.Reverse(p.Rules)
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
		t.Fatal("operational replay was nondeterministic or copied producer text")
	}
	for _, version := range []string{"1", "2"} {
		p.RuleSetVersion = version
		if _, err := diagnose.Analyze(decoded, p); !errors.Is(err, diagnose.ErrUnsupportedVersion) {
			t.Fatal("old policy silently enabled operational rules")
		}
	}
}

func TestOperationalFindingsRespectCollectionBoundaries(t *testing.T) {
	t.Parallel()
	for _, rule := range []diagnose.RuleID{
		diagnose.RuleBackupFailed, diagnose.RuleBackupAbsent, diagnose.RuleUpgrade, diagnose.RuleRestoreTest,
	} {
		t.Run(string(rule), func(t *testing.T) {
			i := restoreTestInput(t)
			i.Observation.Upgrade = upgrade(t).Observation.Upgrade
			i.Observation.Backup.ConsecutiveFailures = integer(2)
			if rule == diagnose.RuleBackupAbsent {
				i.Observation.Backup.ConsecutiveFailures = integer(0)
				i.Observation.Backup.LastSuccessAt = evidence.Timestamp{State: evidence.ValueUnknown}
			}
			i.Collection.Outcome = evidence.CollectionPartial
			report, coverage := runRule(t, i, rule)
			if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 {
				t.Fatal("partial collection erased independent operational facts")
			}
			for _, outcome := range []evidence.CollectionOutcome{evidence.CollectionDeleted, evidence.CollectionReplaced} {
				i.Collection.Outcome = outcome
				report, coverage = runRule(t, i, rule)
				if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
					t.Fatal("operational finding crossed deletion or replacement")
				}
			}
			i.Collection.Outcome = evidence.CollectionComplete
			p := policy(t)
			p.Rules = []diagnose.RuleID{rule}
			p.ReferenceTime = p.ReferenceTime.Add(p.MaxEvidenceAge + time.Second)
			report, err := diagnose.Analyze(capture(i), p)
			if err != nil || len(report.Findings) != 0 {
				t.Fatal("stale operational evidence produced current findings")
			}
		})
	}
}
