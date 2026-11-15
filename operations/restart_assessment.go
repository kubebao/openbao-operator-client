package operations

import (
	"slices"

	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

const (
	// ReasonRestartBlocked means the cluster state or strategy blocks a restart.
	ReasonRestartBlocked AssessmentReason = "restart_blocked"
	// ReasonRestartStrategy means a StatefulSet's update strategy or partition prevents a full rollout.
	ReasonRestartStrategy AssessmentReason = "restart_strategy_unsupported"
	// ReasonRestartMarker means a workload does not yet carry the planned restart marker.
	ReasonRestartMarker AssessmentReason = "restart_marker_pending"
	// ReasonRolloutRevision means the current and update revisions differ.
	ReasonRolloutRevision AssessmentReason = "rollout_revision_pending"
	// ReasonRolloutReadiness means replica counts or Pod readiness do not yet match the desired count.
	ReasonRolloutReadiness AssessmentReason = "rollout_readiness_pending"
)

// AssessRestart verifies current rollout convergence in supplied evidence. It
// does not establish that this request caused Pod replacement, serving health or
// Raft quorum. The caller supplies complete related reads and final verification.
func AssessRestart(plan RestartPlan, observed evidence.Cluster, related collect.RelatedResult,
	policy AssessmentPolicy,
) (Assessment, error) {
	if err := plan.Validate(); err != nil {
		return Assessment{}, invalidAssessment("plan", err)
	}
	result, err := assessmentTarget(plan.Target, plan.Generation, observed, policy)
	if err != nil || !canAssessConfiguration(result) {
		return result, err
	}
	if observed.Runtime.State != evidence.ObjectReported || observed.Runtime.RestartAt.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return result, nil
	}
	configured := textEquals(observed.Runtime.RestartAt, plan.RestartAt)
	result.Configured = reportedBoolean(configured)
	if !configured {
		result.add(AssessmentPending, ReasonNotConfigured)
		return result, nil
	}
	if restartRestricted(observed) {
		result.add(AssessmentBlocked, ReasonRestartBlocked)
		return result, nil
	}
	if !targetVerified(observed, related.Verification, policy) {
		result.add(AssessmentInsufficient, ReasonTargetUnverified)
		return result, nil
	}
	if related.StatefulSets == nil || related.Pods == nil {
		result.add(AssessmentInsufficient, ReasonReadNotRequested)
		return result, nil
	}
	result.StatefulSetRead = related.StatefulSets.Collection.Outcome
	result.PodRead = related.Pods.Collection.Outcome
	checks := workloadChecks{
		set: func(set evidence.StatefulSet) { restartSet(plan, set, &result) },
		pod: func(pod evidence.Pod, set evidence.StatefulSet) { restartPod(plan, pod, set, &result) },
	}
	if err := assessWorkloads(observed, related, policy, &result, checks); err != nil {
		return Assessment{}, err
	}
	switch result.State {
	case AssessmentObserved:
		result.OperatorObserved = reportedBoolean(true)
	case AssessmentPending:
		result.OperatorObserved = reportedBoolean(false)
	}
	return result, nil
}

// restartSet checks a confirmed StatefulSet's strategy, marker, revisions, and replica counts.
func restartSet(plan RestartPlan, set evidence.StatefulSet, result *Assessment) {
	switch set.UpdateStrategy.State {
	case evidence.ValueReported:
		if !textEquals(set.UpdateStrategy, "RollingUpdate") {
			result.add(AssessmentBlocked, ReasonRestartStrategy)
		}
	case evidence.ValueInvalid:
		result.add(AssessmentInsufficient, ReasonInvalidField)
	default:
		result.add(AssessmentInsufficient, ReasonMissingField)
	}
	if set.RollingPartition.State == evidence.ValueInvalid {
		result.add(AssessmentInsufficient, ReasonInvalidField)
	}
	if set.RollingPartition.Value != nil && *set.RollingPartition.Value != 0 {
		result.add(AssessmentBlocked, ReasonRestartStrategy)
	}
	restartDeletion(set.Metadata, result)
	if !textEquals(set.TemplateRestartAt, plan.RestartAt) {
		result.add(AssessmentPending, ReasonRestartMarker)
	}
	if set.Acknowledgement() != evidence.GenerationCurrent {
		result.add(AssessmentInsufficient, ReasonGeneration)
	}
	if set.UpdateRevision.State != evidence.ValueReported || set.CurrentRevision.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
	} else if !sameControlText(set.UpdateRevision, set.CurrentRevision) {
		result.add(AssessmentPending, ReasonRolloutRevision)
	}
	if set.DesiredReplicas.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return
	}
	desired := *set.DesiredReplicas.Value
	if set.MinReadySeconds.State == evidence.ValueInvalid {
		result.add(AssessmentInsufficient, ReasonInvalidField)
	}
	if set.MinReadySeconds.Value != nil && *set.MinReadySeconds.Value > 0 {
		if set.AvailableReplicas.Value == nil {
			result.add(AssessmentInsufficient, ReasonMissingField)
		} else if *set.AvailableReplicas.Value != desired {
			result.add(AssessmentPending, ReasonRolloutReadiness)
		}
	}
	for _, count := range []evidence.Integer{set.ReadyReplicas, set.UpdatedReplicas, set.CurrentReplicas} {
		// Kubernetes omits zero status counters. A configured zero pool needs no Pods;
		// omitted counters do not become reported values in normalized evidence.
		if desired == 0 && count.State == evidence.ValueUnknown {
			continue
		}
		if count.Value == nil {
			result.add(AssessmentInsufficient, ReasonMissingField)
		} else if *count.Value != desired {
			result.add(AssessmentPending, ReasonRolloutReadiness)
		}
	}
}

func restartDeletion(metadata evidence.WorkloadMetadata, result *Assessment) {
	switch metadata.DeletionTimestamp.State {
	case evidence.ValueInvalid:
		result.add(AssessmentInsufficient, ReasonInvalidField)
	case evidence.ValueReported:
		if metadata.DeletionTimestamp.Value.After(*metadata.Reference.CollectedAt.Value) {
			result.add(AssessmentInsufficient, ReasonInconsistentTime)
		} else {
			result.add(AssessmentPending, ReasonWorkloadDeleting)
		}
	}
}

// restartPod checks a confirmed Pod's deletion, marker, revision, and readiness.
func restartPod(plan RestartPlan, pod evidence.Pod, set evidence.StatefulSet, result *Assessment) {
	restartDeletion(pod.Metadata, result)
	if !textEquals(pod.Metadata.RestartAt, plan.RestartAt) {
		result.add(AssessmentPending, ReasonRestartMarker)
	}
	revision := pod.Metadata.ControllerRevision
	if revision.State != evidence.ValueReported || set.UpdateRevision.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
	} else if !sameControlText(revision, set.UpdateRevision) {
		result.add(AssessmentPending, ReasonRolloutRevision)
	}
	restartPodReady(pod, result)
}

// restartPodReady checks that a Pod is running and its current Ready condition is True.
func restartPodReady(pod evidence.Pod, result *Assessment) {
	if pod.Phase.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
	} else if !textEquals(pod.Phase, "Running") {
		result.add(AssessmentPending, ReasonRolloutReadiness)
	}
	if pod.Conditions.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return
	}
	index := slices.IndexFunc(pod.Conditions.Values, func(condition evidence.Condition) bool {
		return condition.Type == "Ready"
	})
	if index < 0 {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return
	}
	condition := pod.Conditions.Values[index]
	if condition.Acknowledgement(pod.Generation) != evidence.GenerationCurrent {
		result.add(AssessmentInsufficient, ReasonGeneration)
	}
	if transitionAfter(condition, *pod.Metadata.Reference.CollectedAt.Value) {
		result.add(AssessmentInsufficient, ReasonInconsistentTime)
	}
	if condition.Status != evidence.ConditionTrue {
		result.add(AssessmentPending, ReasonRolloutReadiness)
	}
}
