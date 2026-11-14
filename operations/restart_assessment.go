package operations

import (
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

const (
	ReasonRestartBlocked   AssessmentReason = "restart_blocked"
	ReasonRestartStrategy  AssessmentReason = "restart_strategy_unsupported"
	ReasonRestartMarker    AssessmentReason = "restart_marker_pending"
	ReasonRolloutRevision  AssessmentReason = "rollout_revision_pending"
	ReasonRolloutReadiness AssessmentReason = "rollout_readiness_pending"
)

// AssessRestart verifies current rollout convergence in supplied evidence. It
// does not establish that this request caused Pod replacement, serving health or
// Raft quorum. The caller supplies complete related reads and final verification.
func AssessRestart(plan RestartPlan, observed evidence.Cluster, related collect.RelatedResult,
	policy AssessmentPolicy,
) (Assessment, error) {
	if plan.Validate() != nil {
		return Assessment{}, ErrInvalidAssessment
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
	if !maintenanceVerification(observed, related.Verification, policy) {
		result.add(AssessmentInsufficient, ReasonTargetUnverified)
		return result, nil
	}
	if related.StatefulSets == nil || related.Pods == nil {
		result.add(AssessmentInsufficient, ReasonReadNotRequested)
		return result, nil
	}
	result.StatefulSetRead, result.PodRead = related.StatefulSets.Collection.Outcome, related.Pods.Collection.Outcome
	if len(related.StatefulSets.Observations) > policy.MaxCandidates ||
		len(related.Pods.Observations) > policy.MaxCandidates-len(related.StatefulSets.Observations) {
		return Assessment{}, ErrInvalidAssessment
	}
	sets, err := restartSets(plan, observed, related, policy, &result)
	if err != nil {
		return Assessment{}, err
	}
	if err = restartPods(plan, observed, related, sets, policy, &result); err != nil {
		return Assessment{}, err
	}
	if result.State == AssessmentObserved {
		result.OperatorObserved = reportedBoolean(true)
	}
	if result.State == AssessmentPending {
		result.OperatorObserved = reportedBoolean(false)
	}
	return result, nil
}

func restartSets(plan RestartPlan, cluster evidence.Cluster, related collect.RelatedResult, policy AssessmentPolicy,
	result *Assessment,
) (map[string]*maintenanceSet, error) {
	read, values := related.StatefulSets.Collection, related.StatefulSets.Observations
	if read.Scope.Group != "apps" || read.Scope.Version != "v1" || read.Scope.Resource != "statefulsets" ||
		values == nil && read.Outcome == evidence.CollectionComplete {
		return nil, ErrInvalidAssessment
	}
	if err := maintenanceScope(cluster, read, len(values), related.Verification, result); err != nil {
		return nil, err
	}
	sets := map[string]*maintenanceSet{}
	names := map[string]bool{}
	topology := maintenanceTopology(cluster, result)
	for _, set := range values {
		if set.Validate() != nil {
			return nil, ErrInvalidAssessment
		}
		ref := set.Metadata.Reference
		if sets[ref.Identity.UID] != nil || names[ref.Identity.Name] {
			return nil, ErrInvalidAssessment
		}
		names[ref.Identity.Name] = true
		if err := maintenanceReference(ref, read, policy, result); err != nil {
			return nil, err
		}
		association, err := evidence.AssociateStatefulSet(set, cluster.Reference.Identity)
		if err != nil {
			return nil, ErrInvalidAssessment
		}
		sets[ref.Identity.UID] = &maintenanceSet{value: set, association: association}
		switch association.State {
		case evidence.AssociationConfirmed:
			maintenanceSetTopology(set, topology, result)
			restartSet(plan, set, result)
		case evidence.AssociationForeign:
		default:
			result.add(AssessmentInsufficient, ReasonAssociation)
		}
	}
	for _, pool := range topology {
		if !pool.found {
			result.add(AssessmentPending, ReasonWorkloadMissing)
		}
	}
	return sets, nil
}

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

func restartPods(plan RestartPlan, cluster evidence.Cluster, related collect.RelatedResult, sets map[string]*maintenanceSet,
	policy AssessmentPolicy, result *Assessment,
) error {
	read, values := related.Pods.Collection, related.Pods.Observations
	if read.Scope.Group != "" || read.Scope.Version != "v1" || read.Scope.Resource != "pods" ||
		values == nil && read.Outcome == evidence.CollectionComplete {
		return ErrInvalidAssessment
	}
	if err := maintenanceScope(cluster, read, len(values), related.Verification, result); err != nil {
		return err
	}
	uids, names := map[string]bool{}, map[string]bool{}
	for _, pod := range values {
		if pod.Validate() != nil {
			return ErrInvalidAssessment
		}
		ref := pod.Metadata.Reference
		if uids[ref.Identity.UID] || names[ref.Identity.Name] {
			return ErrInvalidAssessment
		}
		uids[ref.Identity.UID], names[ref.Identity.Name] = true, true
		if err := maintenanceReference(ref, read, policy, result); err != nil {
			return err
		}
		if err := restartPod(plan, pod, sets, result); err != nil {
			return err
		}
	}
	for _, set := range sets {
		if set.association.State == evidence.AssociationConfirmed && set.value.DesiredReplicas.Value != nil &&
			set.pods != *set.value.DesiredReplicas.Value {
			result.add(AssessmentPending, ReasonWorkloadMissing)
		}
	}
	return nil
}

func restartPod(plan RestartPlan, pod evidence.Pod, sets map[string]*maintenanceSet, result *Assessment) error {
	for _, owner := range pod.Metadata.Owners.Values {
		set := sets[owner.UID]
		if !owner.Controller || set == nil {
			continue
		}
		association, err := evidence.AssociatePod(pod, set.value, set.association)
		if err != nil {
			return ErrInvalidAssessment
		}
		switch association.State {
		case evidence.AssociationConfirmed:
			restartDeletion(pod.Metadata, result)
			if maintenancePodTopology(pod, set.value, result) && pod.Metadata.DeletionTimestamp.State == evidence.ValueUnknown {
				set.pods++
			}
			if !textEquals(pod.Metadata.RestartAt, plan.RestartAt) {
				result.add(AssessmentPending, ReasonRestartMarker)
			}
			if pod.Metadata.ControllerRevision.State != evidence.ValueReported || set.value.UpdateRevision.State != evidence.ValueReported {
				result.add(AssessmentInsufficient, ReasonMissingField)
			} else if !sameControlText(pod.Metadata.ControllerRevision, set.value.UpdateRevision) {
				result.add(AssessmentPending, ReasonRolloutRevision)
			}
			restartPodReady(pod, result)
		case evidence.AssociationForeign:
		default:
			result.add(AssessmentInsufficient, ReasonAssociation)
		}
		return nil
	}
	excluded, err := evidence.PodHasNonStatefulSetController(pod)
	if err != nil {
		return ErrInvalidAssessment
	}
	if !excluded {
		result.add(AssessmentInsufficient, ReasonAssociation)
	}
	return nil
}

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
	for _, condition := range pod.Conditions.Values {
		if condition.Type != "Ready" {
			continue
		}
		if condition.Acknowledgement(pod.Generation) != evidence.GenerationCurrent {
			result.add(AssessmentInsufficient, ReasonGeneration)
		}
		if condition.LastTransitionAt.State == evidence.ValueInvalid || condition.LastTransitionAt.Value != nil &&
			condition.LastTransitionAt.Value.After(*pod.Metadata.Reference.CollectedAt.Value) {
			result.add(AssessmentInsufficient, ReasonInconsistentTime)
		}
		if condition.Status != evidence.ConditionTrue {
			result.add(AssessmentPending, ReasonRolloutReadiness)
		}
		return
	}
	result.add(AssessmentInsufficient, ReasonMissingField)
}
