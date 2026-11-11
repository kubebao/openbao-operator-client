package diagnose_test

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

//go:embed testdata/cluster.json
var fixtures embed.FS

func input(t testing.TB) diagnose.ClusterInput {
	t.Helper()
	data, err := fixtures.ReadFile("testdata/cluster.json")
	if err != nil {
		t.Fatal(err)
	}
	var cluster evidence.Cluster
	if err := json.Unmarshal(data, &cluster); err != nil {
		t.Fatal(err)
	}
	if cluster.Validate() != nil {
		t.Fatal("invalid normalized test fixture")
	}
	at := *cluster.Reference.CollectedAt.Value
	return diagnose.ClusterInput{
		Subject: cluster.Reference.Identity, AdapterContract: cluster.AdapterContract, Observation: &cluster,
		Collection: evidence.Collection{
			Scope: evidence.Scope{
				Origin: "cluster-a", Group: "openbao.org", Version: "v1alpha1", Resource: "openbaoclusters",
				Namespace: "team", Name: "bao", Action: evidence.ReadGet,
			}, Outcome: evidence.CollectionComplete, Items: 1,
			Interval: evidence.Interval{StartedAt: at, CompletedAt: at},
		},
	}
}

func policy(t testing.TB) diagnose.Policy {
	t.Helper()
	i := input(t)
	return diagnose.DefaultPolicy(*i.Observation.Reference.CollectedAt.Value, []string{i.AdapterContract})
}

func capture(inputs ...diagnose.ClusterInput) diagnose.Capture {
	return diagnose.Capture{SchemaVersion: evidence.SchemaVersion, Clusters: inputs}
}

func integer(value int64) evidence.Integer {
	return evidence.Integer{State: evidence.ValueReported, Value: &value}
}

func timestamp(value time.Time) evidence.Timestamp {
	return evidence.Timestamp{State: evidence.ValueReported, Value: &value}
}

func TestAnalysisReplayAndInputOrderAreDeterministic(t *testing.T) {
	t.Parallel()
	a, b := input(t), input(t)
	b.Subject.Origin = "cluster-b"
	b.Observation.Reference.Identity = b.Subject
	b.Collection.Scope.Origin = "cluster-b"
	data := capture(a, b)
	p := policy(t)
	before, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	report, err := diagnose.Analyze(data, p)
	if err != nil || len(report.Findings) != 4 || len(report.Coverage) != 2*len(diagnose.SupportedRules()) {
		t.Fatalf("findings=%d error=%v", len(report.Findings), err)
	}
	first, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(data)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("analysis changed input evidence")
	}
	slices.Reverse(data.Clusters)
	slices.Reverse(p.Rules)
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var replay diagnose.Capture
	if err := json.Unmarshal(encoded, &replay); err != nil {
		t.Fatal(err)
	}
	again, err := diagnose.Analyze(replay, p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(again)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("serialized replay changed report order or metadata")
	}
	if report.Findings[0].Severity != diagnose.Critical {
		t.Fatal("severity did not determine finding order")
	}
}

func TestIncompleteCoverageCanRetainSupportedFindings(t *testing.T) {
	t.Parallel()
	i := input(t)
	i.Collection.Outcome = evidence.CollectionDenied
	report, err := diagnose.Analyze(capture(i), policy(t))
	if err != nil || len(report.Findings) != 2 {
		t.Fatal("incomplete scope discarded independent observed facts")
	}
	for _, entry := range report.Coverage {
		if entry.State != diagnose.Insufficient || entry.Reason != diagnose.ReasonScopeIncomplete {
			t.Fatal("denied scope became evaluated")
		}
	}
	i.Collection.Interval.StartedAt = i.Collection.Interval.StartedAt.Add(time.Second)
	i.Collection.Interval.CompletedAt = i.Collection.Interval.CompletedAt.Add(time.Second)
	p := policy(t)
	p.ReferenceTime = p.ReferenceTime.Add(time.Second)
	report, err = diagnose.Analyze(capture(i), p)
	if err != nil || len(report.Findings) != 0 {
		t.Fatal("last-known facts outside the current attempt produced current findings")
	}
	i = input(t)
	i.Observation = nil
	i.Collection.Outcome = evidence.CollectionDenied
	report, err = diagnose.Analyze(capture(i), policy(t))
	if err != nil || len(report.Findings) != 0 || report.Coverage[0].State != diagnose.Insufficient {
		t.Fatal("missing observation became a healthy result")
	}
}

func TestReadinessRequiresCurrentAcknowledgementAndKnownFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		change   func(*evidence.Cluster)
		state    diagnose.CoverageState
		severity diagnose.Severity
	}{
		{"zero", func(*evidence.Cluster) {}, diagnose.Evaluated, diagnose.Critical},
		{"partial", func(c *evidence.Cluster) { c.ReadyReplicas = integer(1) }, diagnose.Evaluated, diagnose.Warning},
		{"ready", func(c *evidence.Cluster) { c.ReadyReplicas = integer(3) }, diagnose.Evaluated, ""},
		{"stale", func(c *evidence.Cluster) { c.ObservedGeneration = integer(3) }, diagnose.Insufficient, ""},
		{"future", func(c *evidence.Cluster) { c.ObservedGeneration = integer(5) }, diagnose.Insufficient, ""},
		{"unknown", func(c *evidence.Cluster) {
			c.ReadyReplicas = evidence.Integer{State: evidence.ValueUnknown}
		}, diagnose.Insufficient, ""},
		{"paused", func(c *evidence.Cluster) {
			value := true
			c.Paused = evidence.Boolean{State: evidence.ValueReported, Value: &value}
		}, diagnose.Evaluated, ""},
		{"startup", func(c *evidence.Cluster) {
			c.CreatedAt = timestamp(c.Reference.CollectedAt.Value.Add(-30 * time.Second))
		}, diagnose.Evaluated, ""},
		{"grace-equality", func(c *evidence.Cluster) {
			c.CreatedAt = timestamp(c.Reference.CollectedAt.Value.Add(-time.Minute))
		}, diagnose.Evaluated, diagnose.Critical},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, p := input(t), policy(t)
			tc.change(i.Observation)
			p.Rules = []diagnose.RuleID{diagnose.RuleReadiness}
			report, err := diagnose.Analyze(capture(i), p)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range report.Coverage {
				if entry.Rule == diagnose.RuleReadiness && entry.State != tc.state {
					t.Fatal("wrong readiness coverage")
				}
			}
			severity := diagnose.Severity("")
			if len(report.Findings) > 0 {
				severity = report.Findings[0].Severity
			}
			if severity != tc.severity {
				t.Fatalf("severity=%s, want %s", severity, tc.severity)
			}
		})
	}
}

func TestBackupAgeHasStrictThresholdAndPreservesPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		change   func(*evidence.Cluster)
		state    diagnose.CoverageState
		findings int
	}{
		{"overdue", func(*evidence.Cluster) {}, diagnose.Evaluated, 1},
		{"equal", func(c *evidence.Cluster) {
			c.Backup.LastSuccessAt = timestamp(c.Reference.CollectedAt.Value.Add(-24 * time.Hour))
		}, diagnose.Evaluated, 0},
		{"future", func(c *evidence.Cluster) {
			c.Backup.LastSuccessAt = timestamp(c.Reference.CollectedAt.Value.Add(time.Second))
		}, diagnose.Insufficient, 0},
		{"missing-counter", func(c *evidence.Cluster) {
			c.Backup.ConsecutiveFailures = evidence.Integer{State: evidence.ValueUnknown}
		}, diagnose.Insufficient, 0},
		{"failure-precedence", func(c *evidence.Cluster) { c.Backup.ConsecutiveFailures = integer(1) }, diagnose.Evaluated, 0},
		{"unconfigured", func(c *evidence.Cluster) { c.Backup.Configuration = evidence.ObjectAbsent }, diagnose.NotApplicable, 0},
		{"unknown-config", func(c *evidence.Cluster) { c.Backup.Configuration = evidence.ObjectUnknown }, diagnose.Insufficient, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, p := input(t), policy(t)
			tc.change(i.Observation)
			p.Rules = []diagnose.RuleID{diagnose.RuleBackupAge}
			report, err := diagnose.Analyze(capture(i), p)
			if err != nil || len(report.Findings) != tc.findings {
				t.Fatalf("findings=%d error=%v", len(report.Findings), err)
			}
			for _, entry := range report.Coverage {
				if entry.Rule == diagnose.RuleBackupAge && entry.State != tc.state {
					t.Fatal("wrong backup-age coverage")
				}
			}
		})
	}
}

func TestAnalysisRejectsUnknownVersionsAndAmbiguousInputs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		change func(*diagnose.Capture, *diagnose.Policy)
		want   error
	}{
		{func(c *diagnose.Capture, _ *diagnose.Policy) { c.SchemaVersion = "future" }, diagnose.ErrUnsupportedVersion},
		{func(_ *diagnose.Capture, p *diagnose.Policy) { p.RuleSetVersion = "future" }, diagnose.ErrUnsupportedVersion},
		{func(c *diagnose.Capture, _ *diagnose.Policy) {
			c.Clusters[0].AdapterContract = "future"
			c.Clusters[0].Observation.AdapterContract = "future"
		}, diagnose.ErrUnsupportedVersion},
		{func(_ *diagnose.Capture, p *diagnose.Policy) { p.Rules = []diagnose.RuleID{"RAW_CANARY"} }, diagnose.ErrUnknownRule},
		{func(_ *diagnose.Capture, p *diagnose.Policy) { p.Rules = []diagnose.RuleID{diagnose.RuleRecovery} }, diagnose.ErrUnavailableRule},
		{func(c *diagnose.Capture, _ *diagnose.Policy) { c.Clusters = append(c.Clusters, c.Clusters[0]) }, diagnose.ErrDuplicateSubject},
		{func(_ *diagnose.Capture, p *diagnose.Policy) { p.MaxEvidenceAge = 0 }, diagnose.ErrInvalidPolicy},
		{func(c *diagnose.Capture, _ *diagnose.Policy) { c.Clusters[0].Collection.Scope.Namespace = "other" }, diagnose.ErrInvalidCapture},
	} {
		c, p := capture(input(t)), policy(t)
		tc.change(&c, &p)
		_, err := diagnose.Analyze(c, p)
		if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "CANARY") {
			t.Fatalf("error=%v, want %v", err, tc.want)
		}
	}
}

func FuzzAnalyzeNormalizedCapture(f *testing.F) {
	encoded, err := json.Marshal(capture(input(f)))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	signals := input(f)
	setConditions(signals.Observation, condition("Available", evidence.ConditionFalse, 4),
		condition("OpenBaoSealed", evidence.ConditionTrue, 4), condition("TLSReady", evidence.ConditionFalse, 4))
	signals.Observation.Maintenance = evidence.Maintenance{State: evidence.ObjectReported, Enabled: flag(true)}
	signals.Observation.BreakGlass = evidence.BreakGlass{
		State: evidence.ObjectReported, Active: flag(true),
		Reason: text("REASON_CANARY"), EnteredAt: evidence.Timestamp{State: evidence.ValueUnknown},
	}
	withSignals, err := json.Marshal(capture(signals))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(withSignals)
	operations := restoreTestInput(f)
	operations.Observation.Upgrade = upgrade(f).Observation.Upgrade
	operations.Observation.Backup.ConsecutiveFailures = integer(2)
	withOperations, err := json.Marshal(capture(operations))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(withOperations)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1024*1024 {
			return
		}
		var value diagnose.Capture
		if json.Unmarshal(data, &value) != nil {
			return
		}
		report, err := diagnose.Analyze(value, policy(t))
		if err == nil {
			for _, finding := range report.Findings {
				if finding.Validate() != nil {
					t.Fatal("invalid finding")
				}
			}
			if _, err := json.Marshal(report); err != nil {
				t.Fatal(err)
			}
		}
	})
}
