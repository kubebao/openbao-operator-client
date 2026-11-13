package operations_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

var assessmentTime = time.Date(2026, time.November, 11, 10, 0, 0, 0, time.UTC)

func assessmentCluster(t testing.TB, at time.Time, generation int64, paused, maintenance bool) evidence.Cluster {
	t.Helper()
	condition, reason := "True", "Ready"
	if paused {
		condition, reason = "Unknown", "Paused"
	}
	raw := fmt.Sprintf(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
	"metadata":{"namespace":"team","name":"bao","uid":"cluster-1","resourceVersion":"%d","generation":%d},
	"spec":{"version":"2.7.2","replicas":1,"paused":%t,"maintenance":{"enabled":%t}},
	"status":{"observedGeneration":%d,"conditions":[{"type":"Available","status":%q,"reason":%q,"observedGeneration":%d}]}}`,
		generation, generation, paused, maintenance, generation, condition, reason, generation)
	value, err := adapter.ProjectCluster([]byte(raw), assessmentOptions(at, adapter.OpenBaoV1Alpha1))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assessmentOptions(at time.Time, contract adapter.Contract) adapter.ProjectionOptions {
	return adapter.ProjectionOptions{
		Origin: "origin-a", Contract: contract, CollectedAt: at, Limits: adapter.DefaultProjectionLimits(),
	}
}

func assessmentPlan(t testing.TB, paused bool, enabled bool) operations.Plan {
	t.Helper()
	before := assessmentCluster(t, assessmentTime, 1, !paused, !enabled)
	var plan operations.Plan
	var err error
	if paused {
		plan, err = operations.PreparePause(before, adapter.OpenBaoV1Alpha1, enabled)
	} else {
		plan, err = operations.PrepareMaintenance(before, adapter.OpenBaoV1Alpha1, enabled)
	}
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assessmentPolicy() operations.AssessmentPolicy {
	return operations.DefaultAssessmentPolicy(assessmentTime.Add(10 * time.Second))
}

func assessmentInteger(value int64) evidence.Integer {
	return evidence.Integer{State: evidence.ValueReported, Value: &value}
}

func assessmentRelated(t testing.TB, cluster evidence.Cluster, enabled bool) collect.RelatedResult {
	t.Helper()
	at := cluster.Reference.CollectedAt.Value.Add(time.Second)
	annotation := ""
	if enabled {
		annotation = `,"openbao.org/maintenance":"true"`
	}
	setRaw := fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"name":"bao","namespace":"team",
	"uid":"set-1","resourceVersion":"50","generation":2,"annotations":{"openbao.org/owner-uid":"cluster-1"%s},
	"ownerReferences":[{"apiVersion":%q,"kind":"OpenBaoCluster","name":"bao","uid":"cluster-1","controller":true}]},
	"spec":{"replicas":1,"selector":{"matchLabels":{"openbao.org/workload-pool":"voter"}},
	"template":{"metadata":{"labels":{"openbao.org/workload-pool":"voter"}}}},
	"status":{"observedGeneration":2,"readyReplicas":1}}`, annotation, cluster.Reference.APIVersion)
	set, err := adapter.ProjectStatefulSet([]byte(setRaw), assessmentOptions(at, adapter.KubernetesAppsV1))
	if err != nil {
		t.Fatal(err)
	}
	podRaw := fmt.Sprintf(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"bao-0","namespace":"team","uid":"pod-1",
	"resourceVersion":"51","generation":1,"labels":{"openbao.org/workload-pool":"voter"},
	"annotations":{"openbao.org/owner-uid":"cluster-1"%s},
	"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"bao","uid":"set-1","controller":true}]}}`, annotation)
	pod, err := adapter.ProjectPod([]byte(podRaw), assessmentOptions(at, adapter.KubernetesCoreV1))
	if err != nil {
		t.Fatal(err)
	}
	verificationAt := at.Add(time.Second)
	after := cluster
	after.Reference.CollectedAt.Value = &verificationAt
	read := evidence.Collection{
		Scope: evidence.Scope{
			Origin: "origin-a", Group: cluster.Reference.Identity.Group, Version: "v1alpha1", Resource: "openbaoclusters",
			Namespace: "team", Name: "bao", Action: evidence.ReadGet,
		},
		Interval: evidence.Interval{StartedAt: verificationAt, CompletedAt: verificationAt}, Outcome: evidence.CollectionComplete, Items: 1,
	}
	verification, err := evidence.NewTargetVerification(cluster, read, []evidence.Cluster{after})
	if err != nil {
		t.Fatal(err)
	}
	return collect.RelatedResult{
		Verification: verification,
		StatefulSets: &collect.Result[evidence.StatefulSet]{
			Collection: assessmentCollection("apps", "statefulsets", at), Observations: []evidence.StatefulSet{set},
		},
		Pods: &collect.Result[evidence.Pod]{Collection: assessmentCollection("", "pods", at), Observations: []evidence.Pod{pod}},
	}
}

func assessmentCollection(group, resource string, at time.Time) evidence.Collection {
	return evidence.Collection{
		Scope: evidence.Scope{
			Origin: "origin-a", Group: group, Version: "v1", Resource: resource,
			Namespace: "team", Action: evidence.ReadList, Selector: "openbao.org/cluster=bao",
		},
		Interval: evidence.Interval{StartedAt: at, CompletedAt: at}, Outcome: evidence.CollectionComplete, Items: 1,
	}
}

func TestAssessPauseNeedsProducerAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		before := assessmentCluster(t, assessmentTime, 1, !enabled, false)
		plan, err := operations.PreparePause(before, adapter.OpenBaoV1Alpha1, enabled)
		if err != nil {
			t.Fatal(err)
		}
		observed := assessmentCluster(t, assessmentTime.Add(time.Second), 2, enabled, false)
		got, err := operations.AssessPause(plan, observed, assessmentPolicy())
		if err != nil || got.State != operations.AssessmentObserved || got.OperatorObserved.Value == nil || !*got.OperatorObserved.Value {
			t.Fatalf("pause=%t: %+v, %v", enabled, got, err)
		}
		observed.Conditions.State, observed.Conditions.Values = evidence.ValueUnknown, nil
		got, err = operations.AssessPause(plan, observed, assessmentPolicy())
		if err != nil || got.State != operations.AssessmentInsufficient || got.Configured.Value == nil || !*got.Configured.Value {
			t.Fatalf("generation alone acknowledged pause=%t: %+v, %v", enabled, got, err)
		}
		observed = assessmentCluster(t, assessmentTime.Add(time.Second), 2, enabled, false)
		observed.Conditions.Values[0].ObservedGeneration = assessmentInteger(1)
		got, err = operations.AssessPause(plan, observed, assessmentPolicy())
		if err != nil || got.State != operations.AssessmentInsufficient {
			t.Fatalf("old condition acknowledged pause=%t: %+v, %v", enabled, got, err)
		}
	}
}

func TestAssessPauseSeparatesConfigurationAndObservation(t *testing.T) {
	t.Parallel()
	plan := assessmentPlan(t, true, true)
	for _, tc := range []struct {
		name   string
		change func(*evidence.Cluster)
		state  operations.AssessmentState
	}{
		{"not-configured", func(c *evidence.Cluster) { c.Paused.Value = new(bool) }, operations.AssessmentPending},
		{"old-status", func(c *evidence.Cluster) {
			c.ObservedGeneration = assessmentInteger(1)
			c.ReadyReplicas = assessmentInteger(0)
		}, operations.AssessmentInsufficient},
		{"old-generation", func(c *evidence.Cluster) {
			c.Generation = assessmentInteger(0)
			c.ReadyReplicas = assessmentInteger(0)
		}, operations.AssessmentInsufficient},
		{"replacement", func(c *evidence.Cluster) { c.Reference.Identity.UID = "replacement" }, operations.AssessmentInsufficient},
		{"wrong-origin", func(c *evidence.Cluster) { c.Reference.Identity.Origin = "other" }, operations.AssessmentInsufficient},
		{
			"missing-resource-version", func(c *evidence.Cluster) {
				c.Reference.ResourceVersion = evidence.Text{State: evidence.ValueUnknown}
			},
			operations.AssessmentInsufficient,
		},
		{
			"future-read", func(c *evidence.Cluster) { at := assessmentTime.Add(time.Hour); c.Reference.CollectedAt.Value = &at },
			operations.AssessmentInsufficient,
		},
		{
			"pre-plan-read", func(c *evidence.Cluster) { at := assessmentTime; c.Reference.CollectedAt.Value = &at },
			operations.AssessmentInsufficient,
		},
		{
			"old-read", func(c *evidence.Cluster) { at := assessmentTime.Add(time.Second); c.Reference.CollectedAt.Value = &at },
			operations.AssessmentInsufficient,
		},
		{
			"wrong-reason", func(c *evidence.Cluster) { reason := "Ready"; c.Conditions.Values[0].Reason.Value = &reason },
			operations.AssessmentPending,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := assessmentCluster(t, assessmentTime.Add(time.Second), 2, true, false)
			policy := assessmentPolicy()
			if tc.name == "old-read" {
				policy.MaxEvidenceAge = time.Second
			}
			tc.change(&observed)
			got, err := operations.AssessPause(plan, observed, policy)
			if err != nil || got.State != tc.state {
				t.Fatalf("got %+v, %v; want %s", got, err, tc.state)
			}
		})
	}
}

func TestAssessMaintenanceRequiresCompleteFreshPropagation(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		plan := assessmentPlan(t, false, enabled)
		cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, enabled)
		related := assessmentRelated(t, cluster, enabled)
		got, err := operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
		if err != nil || got.State != operations.AssessmentObserved || got.OperatorObserved.Value == nil || !*got.OperatorObserved.Value {
			t.Fatalf("enabled=%t: %+v, %v", enabled, got, err)
		}
		wrong := "true"
		if enabled {
			wrong = "false"
		}
		related.Pods.Observations[0].Metadata.Maintenance = evidence.Text{State: evidence.ValueReported, Value: &wrong}
		got, err = operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
		if err != nil || got.State != operations.AssessmentPending || !slices.Contains(got.Reasons, operations.ReasonAnnotationPending) {
			t.Fatalf("wrong Pod annotation enabled=%t: %+v, %v", enabled, got, err)
		}
	}
}

func TestAssessMaintenanceRetainsInsufficientEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*evidence.Cluster, *collect.RelatedResult)
		reason operations.AssessmentReason
	}{
		{"no-pods", func(_ *evidence.Cluster, r *collect.RelatedResult) { r.Pods = nil }, operations.ReasonReadNotRequested},
		{
			"partial", func(_ *evidence.Cluster, r *collect.RelatedResult) {
				r.Pods.Collection.Outcome = evidence.CollectionPartial
			},
			operations.ReasonReadIncomplete,
		},
		{
			"denied", func(_ *evidence.Cluster, r *collect.RelatedResult) {
				r.StatefulSets.Collection.Outcome = evidence.CollectionDenied
			},
			operations.ReasonReadIncomplete,
		},
		{
			"restricted", func(_ *evidence.Cluster, r *collect.RelatedResult) { r.Pods.Collection.Scope.Selector = "app=bao" },
			operations.ReasonScopeRestricted,
		},
		{"unknown-owner", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Owners = evidence.Owners{State: evidence.ValueUnknown}
		}, operations.ReasonAssociation},
		{"owner-conflict", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Owners.Values[0].Kind = "Job"
			r.Pods.Observations[0].Metadata.Owners.Values[0].APIVersion = "batch/v1"
		}, operations.ReasonAssociation},
		{
			"unverified", func(_ *evidence.Cluster, r *collect.RelatedResult) { r.Verification = evidence.TargetVerification{} },
			operations.ReasonTargetUnverified,
		},
		{"replaced-final-target", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Verification.After.Identity.UID = "replacement"
			r.Verification.State = evidence.VerificationReplaced
		}, operations.ReasonTargetUnverified},
		{"changed-final-generation", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Verification.AfterGeneration = assessmentInteger(3)
			r.Verification.State = evidence.VerificationChanged
		}, operations.ReasonTargetUnverified},
		{"status-only-final-change", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Verification.After.ResourceVersion = assessmentText("new-status-version")
		}, operations.ReasonTargetUnverified},
		{"future-related-read", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			at := assessmentTime.Add(time.Hour)
			r.Pods.Observations[0].Metadata.Reference.CollectedAt.Value = &at
		}, operations.ReasonInconsistentTime},
		{"invalid-annotation", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Maintenance = evidence.Text{State: evidence.ValueInvalid}
		}, operations.ReasonMissingField},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := assessmentPlan(t, false, true)
			cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
			related := assessmentRelated(t, cluster, true)
			tc.change(&cluster, &related)
			got, err := operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
			if err != nil || got.State != operations.AssessmentInsufficient || !slices.Contains(got.Reasons, tc.reason) {
				t.Fatalf("got %+v, %v; want insufficient %s", got, err, tc.reason)
			}
		})
	}
}

func TestAssessMaintenancePausedAndMissingPopulation(t *testing.T) {
	t.Parallel()
	plan := assessmentPlan(t, false, true)
	cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, true, true)
	got, err := operations.AssessMaintenance(plan, cluster, collect.RelatedResult{}, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentBlocked || !slices.Contains(got.Reasons, operations.ReasonPaused) {
		t.Fatalf("paused propagation: %+v, %v", got, err)
	}
	cluster = assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
	related := assessmentRelated(t, cluster, true)
	related.Pods.Collection.Items = 0
	related.Pods.Observations = []evidence.Pod{}
	got, err = operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentPending || !slices.Contains(got.Reasons, operations.ReasonWorkloadMissing) {
		t.Fatalf("empty Pods proved propagation: %+v, %v", got, err)
	}
	related.StatefulSets.Collection.Items = 0
	related.StatefulSets.Observations = []evidence.StatefulSet{}
	got, err = operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentPending {
		t.Fatalf("empty workload proved propagation: %+v, %v", got, err)
	}
	cluster.DesiredReplicas = assessmentInteger(0)
	got, err = operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentInsufficient {
		t.Fatalf("zero expected replicas proved empty propagation: %+v, %v", got, err)
	}
}

func TestAssessMaintenanceRejectsUnconfirmedPoolTopology(t *testing.T) {
	t.Parallel()
	plan := assessmentPlan(t, false, true)
	cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
	cluster.ReadReplicas.Configuration = evidence.ObjectReported
	cluster.ReadReplicas.Desired = assessmentInteger(1)
	related := assessmentRelated(t, cluster, true)
	got, err := operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentPending || !slices.Contains(got.Reasons, operations.ReasonWorkloadMissing) {
		t.Fatalf("missing read replica pool proved propagation: %+v, %v", got, err)
	}
	readSet := related.StatefulSets.Observations[0]
	readSet.Metadata.Reference.Identity.Name, readSet.Metadata.Reference.Identity.UID = "bao-read", "set-read"
	readSet.SelectorPool, readSet.TemplatePool = assessmentText("read-replica"), assessmentText("read-replica")
	related.StatefulSets.Observations = append(related.StatefulSets.Observations, readSet)
	related.StatefulSets.Collection.Items++
	readPod := related.Pods.Observations[0]
	readPod.Metadata.Reference.Identity.Name, readPod.Metadata.Reference.Identity.UID = "bao-read-0", "pod-read"
	readPod.Metadata.Pool = assessmentText("read-replica")
	readPod.Metadata.Owners.Values = []evidence.OwnerReference{{
		APIVersion: "apps/v1", Kind: "StatefulSet", Name: "bao-read", UID: "set-read", Controller: true,
	}}
	related.Pods.Observations = append(related.Pods.Observations, readPod)
	related.Pods.Collection.Items++
	jobPod := readPod
	jobPod.Metadata.Reference.Identity.Name, jobPod.Metadata.Reference.Identity.UID = "backup", "job-pod"
	jobPod.Metadata.Owners.Values = []evidence.OwnerReference{{
		APIVersion: "batch/v1", Kind: "Job", Name: "backup", UID: "backup-job", Controller: true,
	}}
	jobPod.Metadata.Maintenance = evidence.Text{State: evidence.ValueUnknown}
	related.Pods.Observations = append(related.Pods.Observations, jobPod)
	related.Pods.Collection.Items++
	got, err = operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentObserved {
		t.Fatalf("labelled UID-confirmed pools did not establish propagation: %+v, %v", got, err)
	}
	cluster.ReadReplicas.Configuration = evidence.ObjectAbsent
	cluster.ReadReplicas.Desired = evidence.Integer{State: evidence.ValueUnknown}
	got, err = operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentInsufficient || !slices.Contains(got.Reasons, operations.ReasonTopologyConflict) {
		t.Fatalf("unconfigured read replica pool proved topology: %+v %v", got, err)
	}
	related = assessmentRelated(t, cluster, true)
	related.Pods.Observations = append(related.Pods.Observations, jobPod)
	related.Pods.Collection.Items++
	got, err = operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil || got.State != operations.AssessmentObserved {
		t.Fatalf("Job Pod blocked propagated single voter pool: %+v, %v", got, err)
	}
}

func TestAssessMaintenanceRejectsBlueGreenTopology(t *testing.T) {
	t.Parallel()
	for _, state := range []evidence.ObjectState{evidence.ObjectReported, evidence.ObjectUnknown, evidence.ObjectInvalid} {
		plan := assessmentPlan(t, false, true)
		cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
		cluster.BlueGreen.State = state
		related := assessmentRelated(t, cluster, true)
		got, err := operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
		reason := operations.ReasonMissingField
		if state == evidence.ObjectInvalid {
			reason = operations.ReasonInvalidField
		}
		if err != nil || got.State != operations.AssessmentInsufficient || !slices.Contains(got.Reasons, reason) {
			t.Fatalf("blue/green state %s proved topology: %+v %v", state, got, err)
		}
	}
}

func TestAssessMaintenanceReplayAndInputValidation(t *testing.T) {
	t.Parallel()
	plan := assessmentPlan(t, false, true)
	cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
	related := assessmentRelated(t, cluster, true)
	encoded, err := json.Marshal(related)
	if err != nil {
		t.Fatal(err)
	}
	var replayed collect.RelatedResult
	if err := json.Unmarshal(encoded, &replayed); err != nil {
		t.Fatal(err)
	}
	first, err := operations.AssessMaintenance(plan, cluster, related, assessmentPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := operations.AssessMaintenance(plan, cluster, replayed, assessmentPolicy())
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil || !bytes.Equal(a, b) || second.State != operations.AssessmentObserved {
		t.Fatalf("replay changed assessment: %s %s %v", a, b, err)
	}
	for _, policy := range []operations.AssessmentPolicy{
		{},
		{ReferenceTime: assessmentTime, MaxEvidenceAge: 0, MaxCandidates: 1},
		{ReferenceTime: assessmentTime, MaxEvidenceAge: time.Second, MaxCandidates: 0},
	} {
		if _, err := operations.AssessMaintenance(plan, cluster, related, policy); !errors.Is(err, operations.ErrInvalidAssessment) {
			t.Fatalf("invalid policy: %v", err)
		}
	}
	policy := assessmentPolicy()
	policy.MaxCandidates = 1
	if _, err := operations.AssessMaintenance(plan, cluster, related, policy); !errors.Is(err, operations.ErrInvalidAssessment) {
		t.Fatalf("candidate bound: %v", err)
	}
	related.Pods.Observations = append(related.Pods.Observations, related.Pods.Observations[0])
	related.Pods.Collection.Items++
	if _, err := operations.AssessMaintenance(
		plan,
		cluster,
		related,
		assessmentPolicy(),
	); !errors.Is(
		err,
		operations.ErrInvalidAssessment,
	) {
		t.Fatalf("duplicate Pod: %v", err)
	}
}
