package operations_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

func assessmentText(value string) evidence.Text {
	return evidence.Text{State: evidence.ValueReported, Value: &value}
}

func addReadPool(cluster *evidence.Cluster, related *collect.RelatedResult, replicas int64) {
	cluster.ReadReplicas.Configuration = evidence.ObjectReported
	cluster.ReadReplicas.Desired = assessmentInteger(replicas)
	set := related.StatefulSets.Observations[0]
	set.Metadata.Reference.Identity.Name, set.Metadata.Reference.Identity.UID = "read-set", "read-set-uid"
	set.SelectorPool, set.TemplatePool = assessmentText("read-replica"), assessmentText("read-replica")
	set.SelectorRevision, set.TemplateRevision = evidence.Text{State: evidence.ValueUnknown}, evidence.Text{State: evidence.ValueUnknown}
	set.DesiredReplicas, set.ReadyReplicas = assessmentInteger(replicas), assessmentInteger(replicas)
	related.StatefulSets.Observations = append(related.StatefulSets.Observations, set)
	related.StatefulSets.Collection.Items++
	for index := int64(0); index < replicas; index++ {
		pod := related.Pods.Observations[0]
		// Tests use at most one read Pod. Identity does not select its pool.
		pod.Metadata.Reference.Identity.Name, pod.Metadata.Reference.Identity.UID = "read-pod", "read-pod-uid"
		pod.Metadata.Pool = assessmentText("read-replica")
		pod.Metadata.Revision = evidence.Text{State: evidence.ValueUnknown}
		pod.Metadata.Owners.Values = []evidence.OwnerReference{{
			APIVersion: "apps/v1", Kind: "StatefulSet", Name: "read-set", UID: "read-set-uid", Controller: true,
		}}
		related.Pods.Observations = append(related.Pods.Observations, pod)
		related.Pods.Collection.Items++
	}
}

func idleRevision(cluster *evidence.Cluster, related *collect.RelatedResult, revision string) {
	cluster.BlueGreen.State, cluster.BlueGreen.Phase = evidence.ObjectReported, assessmentText("Idle")
	value := evidence.Text{State: evidence.ValueUnknown}
	if revision != "" {
		value = assessmentText(revision)
	}
	cluster.BlueGreen.BlueRevision = value
	cluster.BlueGreen.GreenRevision = evidence.Text{State: evidence.ValueUnknown}
	related.StatefulSets.Observations[0].SelectorRevision = value
	related.StatefulSets.Observations[0].TemplateRevision = value
	related.Pods.Observations[0].Metadata.Revision = value
}

func assertTopologyAssessment(t *testing.T, cluster evidence.Cluster, related collect.RelatedResult,
	enabled bool, state operations.AssessmentState, reason operations.AssessmentReason,
) operations.Assessment {
	t.Helper()
	got, err := operations.AssessMaintenance(assessmentPlan(t, false, enabled), cluster, related, assessmentPolicy())
	if err != nil || got.State != state || reason != "" && !slices.Contains(got.Reasons, reason) {
		t.Fatalf("assessment = %+v, %v; want %s with %s", got, err, state, reason)
	}
	if state != operations.AssessmentObserved && got.OperatorObserved.Value != nil && *got.OperatorObserved.Value {
		t.Fatal("incomplete topology reported complete propagation")
	}
	if state == operations.AssessmentObserved && (got.OperatorObserved.Value == nil || !*got.OperatorObserved.Value) {
		t.Fatal("observed topology did not confirm operator propagation")
	}
	return got
}

func TestMaintenanceReadReplicaPoolsAndZeroPopulation(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		for _, replicas := range []int64{0, 1} {
			cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, enabled)
			related := assessmentRelated(t, cluster, enabled)
			addReadPool(&cluster, &related, replicas)
			assertTopologyAssessment(t, cluster, related, enabled, operations.AssessmentObserved, "")
			// Configured zero retains a zero-sized StatefulSet in the pinned operator.
			related.StatefulSets.Observations = related.StatefulSets.Observations[:1]
			related.StatefulSets.Collection.Items--
			related.Pods.Observations = related.Pods.Observations[:1]
			related.Pods.Collection.Items = 1
			assertTopologyAssessment(t, cluster, related, enabled, operations.AssessmentPending, operations.ReasonWorkloadMissing)
		}
	}
}

func TestMaintenancePoolPresenceCannotBeReplacedByAggregateCounts(t *testing.T) {
	t.Parallel()
	cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
	related := assessmentRelated(t, cluster, true)
	cluster.ReadReplicas.Configuration, cluster.ReadReplicas.Desired = evidence.ObjectReported, assessmentInteger(1)
	second := related.StatefulSets.Observations[0]
	second.Metadata.Reference.Identity.Name, second.Metadata.Reference.Identity.UID = "extra-voter", "extra-voter-uid"
	related.StatefulSets.Observations = append(related.StatefulSets.Observations, second)
	related.StatefulSets.Collection.Items++
	assertTopologyAssessment(t, cluster, related, true, operations.AssessmentInsufficient, operations.ReasonTopologyAmbiguous)

	related = assessmentRelated(t, cluster, true)
	addReadPool(&cluster, &related, 1)
	cluster.ReadReplicas.Configuration, cluster.ReadReplicas.Desired = evidence.ObjectAbsent, evidence.Integer{State: evidence.ValueUnknown}
	assertTopologyAssessment(t, cluster, related, true, operations.AssessmentInsufficient, operations.ReasonTopologyConflict)
}

func TestMaintenanceCountsAreCheckedWithinEachPool(t *testing.T) {
	t.Parallel()
	cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
	related := assessmentRelated(t, cluster, true)
	addReadPool(&cluster, &related, 1)
	// The sum remains two, but one pool has not reached its own desired count.
	related.StatefulSets.Observations[0].DesiredReplicas = assessmentInteger(2)
	related.StatefulSets.Observations[1].DesiredReplicas = assessmentInteger(0)
	assertTopologyAssessment(t, cluster, related, true, operations.AssessmentPending, operations.ReasonWorkloadMissing)
}

func TestMaintenanceMissingAndInvalidReadReplicaCountRemainDistinct(t *testing.T) {
	t.Parallel()
	for _, state := range []evidence.ValueState{evidence.ValueUnknown, evidence.ValueInvalid} {
		cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
		related := assessmentRelated(t, cluster, true)
		cluster.ReadReplicas.Configuration = evidence.ObjectReported
		cluster.ReadReplicas.Desired = evidence.Integer{State: state}
		reason := operations.ReasonMissingField
		if state == evidence.ValueInvalid {
			reason = operations.ReasonInvalidField
		}
		assertTopologyAssessment(t, cluster, related, true, operations.AssessmentInsufficient, reason)
	}
}

func TestMaintenanceMetadataRequiresConfirmedConsistentRoles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*collect.RelatedResult)
		reason operations.AssessmentReason
	}{
		{"missing-selector-pool", func(r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].SelectorPool = evidence.Text{State: evidence.ValueUnknown}
		}, operations.ReasonMissingField},
		{"missing-template-pool", func(r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].TemplatePool = evidence.Text{State: evidence.ValueUnknown}
		}, operations.ReasonMissingField},
		{"invalid-selector-pool", func(r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].SelectorPool = evidence.Text{State: evidence.ValueInvalid}
		}, operations.ReasonInvalidField},
		{"selector-template-pool-disagreement", func(r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].TemplatePool = assessmentText("read-replica")
		}, operations.ReasonTopologyConflict},
		{"template-only-revision", func(r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].TemplateRevision = assessmentText("user-label")
		}, operations.ReasonTopologyConflict},
		{"missing-pod-pool", func(r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Pool = evidence.Text{State: evidence.ValueUnknown}
		}, operations.ReasonMissingField},
		{"invalid-pod-pool", func(r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Pool = evidence.Text{State: evidence.ValueInvalid}
		}, operations.ReasonInvalidField},
		{"pod-pool-disagreement", func(r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Pool = assessmentText("read-replica")
		}, operations.ReasonTopologyConflict},
		{"pod-only-revision", func(r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Revision = assessmentText("other-revision")
		}, operations.ReasonTopologyConflict},
		{"conflicting-cluster-owner", func(r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].Metadata.ClusterOwnerUID = assessmentText("other-cluster")
		}, operations.ReasonAssociation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
			related := assessmentRelated(t, cluster, true)
			tc.change(&related)
			assertTopologyAssessment(t, cluster, related, true, operations.AssessmentInsufficient, tc.reason)
		})
	}
}

func TestMaintenanceIdleRevisionAndUnsupportedTransitions(t *testing.T) {
	t.Parallel()
	for _, revision := range []string{"", "stable-123"} {
		cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
		related := assessmentRelated(t, cluster, true)
		idleRevision(&cluster, &related, revision)
		// A retained validation hook can retain this ID after successful cleanup.
		cluster.BlueGreen.OperationID = assessmentText("retained-operation")
		assertTopologyAssessment(t, cluster, related, true, operations.AssessmentObserved, "")
		for _, phase := range []string{
			"DeployingGreen", "JoiningMesh", "Syncing", "Promoting", "DemotingBlue", "Cleanup",
			"RestoringReadReplicas", "RollingBack", "RollbackCleanup",
		} {
			cluster.BlueGreen.Phase = assessmentText(phase)
			assertTopologyAssessment(t, cluster, related, true, operations.AssessmentInsufficient, operations.ReasonTopologyUnconfirmed)
		}
	}
}

func TestMaintenanceIdleRevisionConflictsRemainInsufficient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*evidence.Cluster, *collect.RelatedResult)
		reason operations.AssessmentReason
	}{
		{"leftover-green-revision", func(c *evidence.Cluster, _ *collect.RelatedResult) {
			c.BlueGreen.GreenRevision = assessmentText("green")
		}, operations.ReasonTopologyConflict},
		{"invalid-blue-revision", func(c *evidence.Cluster, _ *collect.RelatedResult) {
			c.BlueGreen.BlueRevision = evidence.Text{State: evidence.ValueInvalid}
		}, operations.ReasonInvalidField},
		{"wrong-set-revision", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.StatefulSets.Observations[0].SelectorRevision = assessmentText("other")
			r.StatefulSets.Observations[0].TemplateRevision = assessmentText("other")
		}, operations.ReasonTopologyConflict},
		{"wrong-pod-revision", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Revision = assessmentText("other")
		}, operations.ReasonTopologyConflict},
		{"missing-pod-revision", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Pods.Observations[0].Metadata.Revision = evidence.Text{State: evidence.ValueUnknown}
		}, operations.ReasonMissingField},
		{"status-only-resource-version-change", func(_ *evidence.Cluster, r *collect.RelatedResult) {
			r.Verification.After.ResourceVersion = assessmentText("new-status-version")
		}, operations.ReasonTargetUnverified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
			related := assessmentRelated(t, cluster, true)
			idleRevision(&cluster, &related, "stable")
			tc.change(&cluster, &related)
			assertTopologyAssessment(t, cluster, related, true, operations.AssessmentInsufficient, tc.reason)
		})
	}
}

func TestMaintenanceRoleLabelsDoNotEstablishOwnership(t *testing.T) {
	t.Parallel()
	cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
	related := assessmentRelated(t, cluster, true)
	set := &related.StatefulSets.Observations[0]
	set.Metadata.Owners.Values[0].UID = "foreign-cluster"
	set.Metadata.ClusterOwnerUID = assessmentText("foreign-cluster")
	// A foreign set and its Pod carry the expected role labels, but do not
	// populate this cluster's expected voter slot.
	related.Pods.Observations[0].Metadata.ClusterOwnerUID = assessmentText("foreign-cluster")
	assertTopologyAssessment(t, cluster, related, true, operations.AssessmentPending, operations.ReasonWorkloadMissing)
}

func TestMaintenancePoolPopulationAndOrderReplay(t *testing.T) {
	t.Parallel()
	cluster := assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, true)
	related := assessmentRelated(t, cluster, true)
	addReadPool(&cluster, &related, 1)
	first := assertTopologyAssessment(t, cluster, related, true, operations.AssessmentObserved, "")
	slices.Reverse(related.StatefulSets.Observations)
	slices.Reverse(related.Pods.Observations)
	raw, err := json.Marshal(related)
	if err != nil {
		t.Fatal(err)
	}
	var replay collect.RelatedResult
	if err := json.Unmarshal(raw, &replay); err != nil {
		t.Fatal(err)
	}
	second := assertTopologyAssessment(t, cluster, replay, true, operations.AssessmentObserved, "")
	if !reflect.DeepEqual(first, second) {
		t.Fatal("reordering and replay changed pool propagation assessment")
	}
	// A live Pod in a zero-sized pool means the population has not converged.
	for index := range related.StatefulSets.Observations {
		set := &related.StatefulSets.Observations[index]
		if *set.SelectorPool.Value == "read-replica" {
			set.DesiredReplicas, set.ReadyReplicas = assessmentInteger(0), assessmentInteger(0)
		}
	}
	cluster.ReadReplicas.Desired = assessmentInteger(0)
	assertTopologyAssessment(t, cluster, related, true, operations.AssessmentPending, operations.ReasonWorkloadMissing)
}
