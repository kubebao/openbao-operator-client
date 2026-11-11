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

func workloadInput(t testing.TB) diagnose.ClusterInput {
	t.Helper()
	i := input(t)
	set := readWorkloadFixture[evidence.StatefulSet](t, "statefulset")
	pod := readWorkloadFixture[evidence.Pod](t, "pod")
	i.StatefulSets = &diagnose.ResourceInput[evidence.StatefulSet]{
		AdapterContract: set.AdapterContract, Collection: workloadCollection(i, "apps", "statefulsets"),
		Observations: []evidence.StatefulSet{set},
	}
	i.Pods = &diagnose.ResourceInput[evidence.Pod]{
		AdapterContract: pod.AdapterContract, Collection: workloadCollection(i, "", "pods"), Observations: []evidence.Pod{pod},
	}
	ref := i.Observation.Reference
	at := ref.CollectedAt.Value.Add(time.Second)
	ref.CollectedAt = timestamp(at)
	i.Verification = &evidence.TargetVerification{
		SchemaVersion: evidence.SchemaVersion, AdapterContract: i.AdapterContract, Before: i.Observation.Reference,
		BeforeGeneration: i.Observation.Generation, After: &ref, AfterGeneration: i.Observation.Generation,
		State: evidence.VerificationVerified, Read: evidence.Collection{
			Scope: i.Collection.Scope, Outcome: evidence.CollectionComplete, Items: 1,
			Interval: evidence.Interval{StartedAt: at, CompletedAt: at},
		},
	}
	return i
}

func readWorkloadFixture[T any](t testing.TB, name string) T {
	t.Helper()
	data, err := fixtures.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func workloadCollection(i diagnose.ClusterInput, group, resource string) evidence.Collection {
	return evidence.Collection{
		Scope: evidence.Scope{
			Origin: i.Subject.Origin, Group: group, Version: "v1", Resource: resource, Namespace: i.Subject.Namespace,
			Action: evidence.ReadList, Selector: "openbao.org/cluster=" + i.Subject.Name,
		}, Interval: i.Collection.Interval, Outcome: evidence.CollectionComplete, Items: 1,
	}
}

func workloadPolicy(t testing.TB) diagnose.Policy {
	t.Helper()
	i := workloadInput(t)
	p := policy(t)
	p.ApprovedContracts = append(p.ApprovedContracts, i.StatefulSets.AdapterContract, i.Pods.AdapterContract)
	p.ReferenceTime = p.ReferenceTime.Add(time.Second)
	p.Rules = []diagnose.RuleID{diagnose.RuleWorkload, diagnose.RulePod}
	return p
}

func runWorkloadRule(t *testing.T, i diagnose.ClusterInput, rule diagnose.RuleID) (diagnose.Report, diagnose.Coverage) {
	t.Helper()
	p := workloadPolicy(t)
	p.Rules = []diagnose.RuleID{rule}
	report, err := diagnose.Analyze(capture(i), p)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range report.Coverage {
		if entry.Rule == rule {
			return report, entry
		}
	}
	t.Fatal("workload rule has no coverage entry")
	return diagnose.Report{}, diagnose.Coverage{}
}

func TestWorkloadReadinessRequiresCurrentUIDAssociation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		change   func(*evidence.StatefulSet)
		state    diagnose.CoverageState
		findings int
	}{
		{"current-implied-zero", func(_ *evidence.StatefulSet) {}, diagnose.Evaluated, 1},
		{"stale", func(v *evidence.StatefulSet) {
			v.ObservedGeneration, v.ReadyReplicas = integer(3), integer(0)
		}, diagnose.Insufficient, 0},
		{"future", func(v *evidence.StatefulSet) {
			v.ObservedGeneration, v.ReadyReplicas = integer(5), integer(0)
		}, diagnose.Insufficient, 0},
		{
			"missing-ready", func(v *evidence.StatefulSet) { v.ReadyReplicas = evidence.Integer{State: evidence.ValueUnknown} },
			diagnose.Insufficient, 0,
		},
		{"ready", func(v *evidence.StatefulSet) { v.ReadyReplicas = integer(3) }, diagnose.Evaluated, 0},
		{"zero-desired", func(v *evidence.StatefulSet) { v.DesiredReplicas = integer(0) }, diagnose.Evaluated, 0},
		{"deleting", func(v *evidence.StatefulSet) {
			v.Metadata.DeletionTimestamp = timestamp(*v.Metadata.Reference.CollectedAt.Value)
		}, diagnose.Evaluated, 0},
		{"invalid-deletion", func(v *evidence.StatefulSet) {
			v.Metadata.DeletionTimestamp = evidence.Timestamp{State: evidence.ValueInvalid}
		}, diagnose.Insufficient, 0},
		{
			"missing-owner", func(v *evidence.StatefulSet) { v.Metadata.Owners = evidence.Owners{State: evidence.ValueUnknown} },
			diagnose.Insufficient, 0,
		},
		{
			"conflicting-annotation", func(v *evidence.StatefulSet) { v.Metadata.ClusterOwnerUID = text("replacement") },
			diagnose.Insufficient, 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := workloadInput(t)
			tc.change(&i.StatefulSets.Observations[0])
			report, coverage := runWorkloadRule(t, i, diagnose.RuleWorkload)
			if coverage.State != tc.state || len(report.Findings) != tc.findings {
				t.Fatal("unconfirmed workload readiness became a current finding")
			}
			if tc.findings > 0 && (report.Findings[0].Subject.Kind != "StatefulSet" || report.Findings[0].Severity != diagnose.Warning) {
				t.Fatal("workload finding lost its subject or severity")
			}
		})
	}
}

func TestMissingWorkloadsRequireCompleteSupportedScope(t *testing.T) {
	t.Parallel()
	i := workloadInput(t)
	i.StatefulSets.Observations, i.StatefulSets.Collection.Items = []evidence.StatefulSet{}, 0
	report, coverage := runWorkloadRule(t, i, diagnose.RuleWorkload)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 || report.Findings[0].Subject != i.Subject ||
		report.Findings[0].Subsubject != "statefulset-population" {
		t.Fatal("complete empty workload scope lost its bounded absence finding")
	}
	for _, outcome := range []evidence.CollectionOutcome{
		evidence.CollectionDenied, evidence.CollectionPartial, evidence.CollectionTimeout, evidence.CollectionSizeLimit,
	} {
		i.StatefulSets.Collection.Outcome = outcome
		report, coverage = runWorkloadRule(t, i, diagnose.RuleWorkload)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 || coverage.Outcome != outcome {
			t.Fatal("incomplete workload list became absence")
		}
	}
	i.StatefulSets.Collection.Outcome = evidence.CollectionComplete
	i.StatefulSets.Collection.Scope.Selector = "app=unrelated"
	report, coverage = runWorkloadRule(t, i, diagnose.RuleWorkload)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 || coverage.Reason != diagnose.ReasonScopeRestricted {
		t.Fatal("narrow query became complete workload coverage")
	}
	i = workloadInput(t)
	i.StatefulSets.Collection.Scope.Selector = "app=unrelated"
	report, coverage = runWorkloadRule(t, i, diagnose.RuleWorkload)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 {
		t.Fatal("restricted query erased independent workload evidence")
	}
}

func TestPodReadinessAndFailureHaveSubjectSpecificFindings(t *testing.T) {
	t.Parallel()
	i := workloadInput(t)
	i.StatefulSets.Observations[0].DesiredReplicas = integer(1)
	report, coverage := runWorkloadRule(t, i, diagnose.RulePod)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 || report.Findings[0].Subsubject != "ready" ||
		report.Findings[0].Subject != i.Pods.Observations[0].Metadata.Reference.Identity {
		t.Fatal("Pod readiness lost its identity")
	}
	refs := report.Findings[0].References
	if !slices.ContainsFunc(refs, func(ref evidence.Reference) bool {
		return ref.Field == "status.containerStatuses[name=bao].state.waiting"
	}) {
		t.Fatal("waiting container evidence reference was lost")
	}
	i.Pods.Observations[0].Phase = text("Failed")
	i.Pods.Observations[0].Conditions = evidence.Conditions{State: evidence.ValueUnknown}
	report, coverage = runWorkloadRule(t, i, diagnose.RulePod)
	if len(report.Findings) != 1 || report.Findings[0].Severity != diagnose.Error || report.Findings[0].Subsubject != "phase" ||
		coverage.State != diagnose.Insufficient {
		t.Fatal("failed phase lost its error or hid missing readiness evidence")
	}
}

func TestPodGenerationAndOwnershipStayIndependent(t *testing.T) {
	t.Parallel()
	for _, ack := range []int64{0, 1, 3, -1} {
		i := workloadInput(t)
		i.StatefulSets.Observations[0].DesiredReplicas = integer(1)
		i.Pods.Observations[0].Conditions.Values[0].ObservedGeneration = integer(ack)
		report, coverage := runWorkloadRule(t, i, diagnose.RulePod)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
			t.Fatal("unconfirmed Pod condition generation became Ready=False")
		}
	}
	for _, change := range []func(*diagnose.ClusterInput){
		func(i *diagnose.ClusterInput) { i.Pods.Observations[0].Metadata.ClusterOwnerUID = text("replacement") },
		func(i *diagnose.ClusterInput) {
			i.StatefulSets.Observations[0].Metadata.Reference.Identity.UID = "new-set"
		},
		func(i *diagnose.ClusterInput) {
			i.StatefulSets.Observations[0].Metadata.ClusterOwnerUID = text("replacement")
		},
	} {
		i := workloadInput(t)
		change(&i)
		report, coverage := runWorkloadRule(t, i, diagnose.RulePod)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
			t.Fatal("Pod finding crossed a replaced or conflicting ownership chain")
		}
	}
}

func TestPodPopulationUsesBothCompleteScopes(t *testing.T) {
	t.Parallel()
	i := workloadInput(t)
	i.Pods.Observations[0].Conditions.Values[0].Status = evidence.ConditionTrue
	report, coverage := runWorkloadRule(t, i, diagnose.RulePod)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 || report.Findings[0].Subsubject != "pod-population" ||
		report.Findings[0].Subject != i.StatefulSets.Observations[0].Metadata.Reference.Identity {
		t.Fatal("Pod population finding lost the StatefulSet subject")
	}
	for _, sets := range []bool{true, false} {
		i = workloadInput(t)
		i.Pods.Observations[0].Conditions.Values[0].Status = evidence.ConditionTrue
		if sets {
			i.StatefulSets.Collection.Outcome = evidence.CollectionPartial
		} else {
			i.Pods.Collection.Outcome = evidence.CollectionPartial
		}
		report, coverage = runWorkloadRule(t, i, diagnose.RulePod)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
			t.Fatal("partial dependency scope became missing Pod population")
		}
	}
	i = workloadInput(t)
	i.Pods.Observations[0].Conditions = evidence.Conditions{State: evidence.ValueUnknown}
	report, coverage = runWorkloadRule(t, i, diagnose.RulePod)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 || report.Findings[0].Subsubject != "pod-population" {
		t.Fatal("unknown readiness erased independently confirmed Pod population")
	}
}

func TestWorkloadTargetVerificationAndFreshnessAreRequired(t *testing.T) {
	t.Parallel()
	for _, rule := range []diagnose.RuleID{diagnose.RuleWorkload, diagnose.RulePod} {
		i := workloadInput(t)
		i.Verification = nil
		report, coverage := runWorkloadRule(t, i, rule)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 || coverage.Reason != diagnose.ReasonVerification {
			t.Fatal("unverified workload snapshot produced findings")
		}
		i = workloadInput(t)
		i.Verification.AfterGeneration = integer(5)
		i.Verification.State = evidence.VerificationChanged
		report, coverage = runWorkloadRule(t, i, rule)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
			t.Fatal("changed target generation produced workload findings")
		}
		i = workloadInput(t)
		p := workloadPolicy(t)
		p.Rules = []diagnose.RuleID{rule}
		p.ReferenceTime = p.ReferenceTime.Add(p.MaxEvidenceAge + time.Second)
		report, err := diagnose.Analyze(capture(i), p)
		if err != nil || len(report.Findings) != 0 {
			t.Fatal("old workload snapshot produced current findings")
		}
	}
}

func TestPodDeletionAndIncompleteReadinessDoNotHidePopulation(t *testing.T) {
	t.Parallel()
	i := workloadInput(t)
	i.Pods.Observations[0].Metadata.DeletionTimestamp = timestamp(*i.Observation.Reference.CollectedAt.Value)
	report, coverage := runWorkloadRule(t, i, diagnose.RulePod)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 || report.Findings[0].Subsubject != "pod-population" {
		t.Fatal("terminating Pod was counted or produced a readiness finding")
	}
	i.Pods.Observations[0].Metadata.DeletionTimestamp = evidence.Timestamp{State: evidence.ValueInvalid}
	report, coverage = runWorkloadRule(t, i, diagnose.RulePod)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
		t.Fatal("invalid deletion state became missing Pod population")
	}
	i = workloadInput(t)
	i.Pods.Observations[0].Conditions.Values[0].ObservedGeneration = evidence.Integer{State: evidence.ValueUnknown}
	report, coverage = runWorkloadRule(t, i, diagnose.RulePod)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 || report.Findings[0].Subsubject != "pod-population" {
		t.Fatal("missing condition generation erased independently confirmed population")
	}
}

func TestWorkloadInputRejectsContradictoryEnvelopes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*diagnose.ClusterInput, *diagnose.Policy)
		want   error
	}{
		{
			"namespace", func(i *diagnose.ClusterInput, _ *diagnose.Policy) { i.Pods.Collection.Scope.Namespace = "other" },
			diagnose.ErrInvalidCapture,
		},
		{
			"origin", func(i *diagnose.ClusterInput, _ *diagnose.Policy) { i.StatefulSets.Collection.Scope.Origin = "other" },
			diagnose.ErrInvalidCapture,
		},
		{
			"unapproved-contract", func(i *diagnose.ClusterInput, _ *diagnose.Policy) { i.Pods.AdapterContract = "other" },
			diagnose.ErrUnsupportedVersion,
		},
		{"duplicate", func(i *diagnose.ClusterInput, _ *diagnose.Policy) {
			i.Pods.Observations = append(i.Pods.Observations, i.Pods.Observations[0])
			i.Pods.Collection.Items = 2
		}, diagnose.ErrInvalidCapture},
		{
			"candidate-count", func(i *diagnose.ClusterInput, _ *diagnose.Policy) { i.Pods.Collection.Items = 2 },
			diagnose.ErrInvalidCapture,
		},
		{"future-read", func(i *diagnose.ClusterInput, _ *diagnose.Policy) {
			i.Pods.Observations[0].Metadata.Reference.CollectedAt = timestamp(i.Pods.Collection.Interval.CompletedAt.Add(time.Second))
		}, diagnose.ErrInvalidCapture},
		{"verification-too-early", func(i *diagnose.ClusterInput, _ *diagnose.Policy) {
			i.Pods.Collection.Interval.CompletedAt = i.Verification.Read.Interval.StartedAt.Add(time.Second)
		}, diagnose.ErrInvalidCapture},
		{"subject-budget", func(_ *diagnose.ClusterInput, p *diagnose.Policy) { p.MaxSubjects = 2 }, diagnose.ErrInvalidCapture},
		{"old-policy", func(_ *diagnose.ClusterInput, p *diagnose.Policy) { p.RuleSetVersion = "3" }, diagnose.ErrUnsupportedVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, p := workloadInput(t), workloadPolicy(t)
			tc.change(&i, &p)
			if _, err := diagnose.Analyze(capture(i), p); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWorkloadReplayPreservesOrderingAndExcludesProducerText(t *testing.T) {
	t.Parallel()
	i, p := workloadInput(t), workloadPolicy(t)
	otherSet := readWorkloadFixture[evidence.StatefulSet](t, "statefulset")
	otherSet.Metadata.Reference.Identity.UID, otherSet.Metadata.Reference.Identity.Name = "set-2", "bao-extra"
	otherSet.ObservedGeneration, otherSet.ReadyReplicas = integer(3), integer(0)
	i.StatefulSets.Observations = append(i.StatefulSets.Observations, otherSet)
	i.StatefulSets.Collection.Items = 2
	other := readWorkloadFixture[evidence.Pod](t, "pod")
	other.Metadata.Reference.Identity.UID, other.Metadata.Reference.Identity.Name = "pod-2", "bao-1"
	other.Phase = text("Failed")
	i.Pods.Observations = append(i.Pods.Observations, other)
	i.Pods.Collection.Items = 2
	data, err := json.Marshal(capture(i))
	if err != nil {
		t.Fatal(err)
	}
	var decoded diagnose.Capture
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	first, err := diagnose.Analyze(decoded, p)
	if err != nil || len(first.Findings) != 4 {
		t.Fatalf("workload replay: findings %d error %v", len(first.Findings), err)
	}
	slices.Reverse(decoded.Clusters[0].Pods.Observations)
	slices.Reverse(decoded.Clusters[0].StatefulSets.Observations)
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
	if err != nil || !bytes.Equal(a, b) || strings.Contains(string(a), "CANARY") || strings.Contains(string(a), "CrashLoopBackOff") {
		t.Fatal("workload replay changed order or copied producer diagnostic text")
	}
	if first.Findings[0].Severity != diagnose.Error {
		t.Fatal("failed Pod severity did not determine finding order")
	}
}

func TestForeignPodChainDoesNotLimitTargetCoverage(t *testing.T) {
	t.Parallel()
	for _, conflicting := range []bool{false, true} {
		i := workloadInput(t)
		baseline, baselineCoverage := runWorkloadRule(t, i, diagnose.RulePod)
		foreignSet := readWorkloadFixture[evidence.StatefulSet](t, "statefulset")
		foreignSet.Metadata.Reference.Identity.Name, foreignSet.Metadata.Reference.Identity.UID = "other", "other-set"
		foreignSet.Metadata.Owners.Values[0].Name, foreignSet.Metadata.Owners.Values[0].UID = "other", "other-cluster"
		foreignSet.Metadata.ClusterOwnerUID = text("other-cluster")
		foreignPod := readWorkloadFixture[evidence.Pod](t, "pod")
		foreignPod.Metadata.Reference.Identity.Name, foreignPod.Metadata.Reference.Identity.UID = "other-0", "other-pod"
		foreignPod.Metadata.Owners.Values[0].Name, foreignPod.Metadata.Owners.Values[0].UID = "other", "other-set"
		foreignPod.Metadata.ClusterOwnerUID = text("other-cluster")
		if conflicting {
			foreignPod.Metadata.ClusterOwnerUID = text(i.Subject.UID)
		}
		i.StatefulSets.Observations = append(i.StatefulSets.Observations, foreignSet)
		i.StatefulSets.Collection.Items++
		i.Pods.Observations = append(i.Pods.Observations, foreignPod)
		i.Pods.Collection.Items++
		report, coverage := runWorkloadRule(t, i, diagnose.RulePod)
		if !conflicting && len(report.Findings) != len(baseline.Findings) {
			t.Fatal("foreign Pod changed target findings")
		}
		for _, finding := range report.Findings {
			if finding.Subject.UID == "other-pod" || finding.Subject.UID == "other-set" {
				t.Fatal("foreign workload became a finding")
			}
		}
		if conflicting {
			if coverage.State != diagnose.Insufficient {
				t.Fatal("conflicting annotation became foreign proof")
			}
		} else if coverage.State != baselineCoverage.State {
			t.Fatalf("foreign Pod changed target coverage: %+v", coverage)
		}
	}
}

func TestSuppliedParentCannotOverrideWorkloadMetadata(t *testing.T) {
	t.Parallel()
	i := workloadInput(t)
	set, pod := i.StatefulSets.Observations[0], i.Pods.Observations[0]
	parent, err := evidence.AssociateStatefulSet(set, i.Subject)
	if err != nil {
		t.Fatal(err)
	}
	set.Metadata.Owners.Values[0].UID = "other-cluster"
	set.Metadata.ClusterOwnerUID = text("other-cluster")
	got, err := evidence.AssociatePod(pod, set, parent)
	if err != nil || got.State == evidence.AssociationConfirmed || got.State == evidence.AssociationForeign {
		t.Fatalf("forged parent established proof: %+v %v", got, err)
	}
}
