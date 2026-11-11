package operations

import (
	"errors"
	"slices"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

var ErrInvalidAssessment = errors.New("invalid control assessment")

type AssessmentState string

const (
	AssessmentObserved     AssessmentState = "observed"
	AssessmentPending      AssessmentState = "pending"
	AssessmentBlocked      AssessmentState = "blocked"
	AssessmentInsufficient AssessmentState = "insufficient"
)

type AssessmentReason string

const (
	ReasonTargetChanged       AssessmentReason = "target_changed"
	ReasonTargetDeleting      AssessmentReason = "target_deleting"
	ReasonStaleEvidence       AssessmentReason = "stale_evidence"
	ReasonInconsistentTime    AssessmentReason = "inconsistent_time"
	ReasonMissingField        AssessmentReason = "missing_field"
	ReasonInvalidField        AssessmentReason = "invalid_field"
	ReasonGeneration          AssessmentReason = "generation_unconfirmed"
	ReasonNotConfigured       AssessmentReason = "not_configured"
	ReasonPauseCondition      AssessmentReason = "pause_condition_unconfirmed"
	ReasonPaused              AssessmentReason = "paused"
	ReasonReadNotRequested    AssessmentReason = "read_not_requested"
	ReasonReadIncomplete      AssessmentReason = "read_incomplete"
	ReasonScopeRestricted     AssessmentReason = "scope_restricted"
	ReasonTargetUnverified    AssessmentReason = "target_unverified"
	ReasonAssociation         AssessmentReason = "association_unconfirmed"
	ReasonAnnotationPending   AssessmentReason = "annotation_pending"
	ReasonWorkloadMissing     AssessmentReason = "workload_missing"
	ReasonWorkloadDeleting    AssessmentReason = "workload_deleting"
	ReasonTopologyUnconfirmed AssessmentReason = "topology_unconfirmed"
	ReasonTopologyConflict    AssessmentReason = "topology_conflict"
	ReasonTopologyAmbiguous   AssessmentReason = "topology_ambiguous"
)

// AssessmentPolicy supplies evaluation time and bounds. Assessment reads no clock.
type AssessmentPolicy struct {
	ReferenceTime  time.Time     `json:"referenceTime"`
	MaxEvidenceAge time.Duration `json:"maxEvidenceAge"`
	MaxCandidates  int           `json:"maxCandidates"`
}

func DefaultAssessmentPolicy(reference time.Time) AssessmentPolicy {
	return AssessmentPolicy{ReferenceTime: reference, MaxEvidenceAge: 5 * time.Minute, MaxCandidates: 5000}
}

// Assessment describes the requested state in supplied evidence. Observed does
// not attribute the state to a submitted PATCH, establish authorization, or prove
// that Kubernetes controllers, Jobs, or OpenBao processes have stopped.
// Configured compares the observed spec with the plan. OperatorObserved records
// producer acknowledgement and, for maintenance, complete workload propagation.
type Assessment struct {
	State            AssessmentState            `json:"state"`
	Configured       evidence.Boolean           `json:"configured"`
	OperatorObserved evidence.Boolean           `json:"operatorObserved"`
	Reasons          []AssessmentReason         `json:"reasons"`
	StatefulSetRead  evidence.CollectionOutcome `json:"statefulSetRead"`
	PodRead          evidence.CollectionOutcome `json:"podRead"`
}

func newAssessment() Assessment {
	return Assessment{
		State: AssessmentObserved, Configured: evidence.Boolean{State: evidence.ValueUnknown},
		OperatorObserved: evidence.Boolean{State: evidence.ValueUnknown}, Reasons: []AssessmentReason{},
		StatefulSetRead: evidence.CollectionNotRequested, PodRead: evidence.CollectionNotRequested,
	}
}

func (a *Assessment) add(state AssessmentState, reason AssessmentReason) {
	rank := map[AssessmentState]int{AssessmentObserved: 0, AssessmentPending: 1, AssessmentBlocked: 2, AssessmentInsufficient: 3}
	if rank[state] > rank[a.State] {
		a.State = state
	}
	a.Reasons = append(a.Reasons, reason)
	slices.Sort(a.Reasons)
	a.Reasons = slices.Compact(a.Reasons)
}

func reportedBoolean(value bool) evidence.Boolean {
	return evidence.Boolean{State: evidence.ValueReported, Value: &value}
}

func assessmentHeader(plan Plan, observed evidence.Cluster, policy AssessmentPolicy, control Control) (Assessment, error) {
	result := newAssessment()
	at := policy.ReferenceTime
	if plan.Validate() != nil || plan.Control != control || observed.Validate() != nil ||
		(evidence.Timestamp{State: evidence.ValueReported, Value: &at}).Validate() != nil ||
		policy.MaxEvidenceAge <= 0 || policy.MaxCandidates <= 0 {
		return Assessment{}, ErrInvalidAssessment
	}
	if observed.AdapterContract != plan.AdapterContract || observed.Reference.Identity != plan.Target.Identity ||
		observed.Reference.APIVersion != plan.Target.APIVersion {
		result.add(AssessmentInsufficient, ReasonTargetChanged)
		return result, nil
	}
	if !observed.Reference.CollectedAt.Value.After(*plan.Target.CollectedAt.Value) || observed.Reference.CollectedAt.Value.After(at) {
		result.add(AssessmentInsufficient, ReasonInconsistentTime)
	}
	if at.Sub(*observed.Reference.CollectedAt.Value) > policy.MaxEvidenceAge {
		result.add(AssessmentInsufficient, ReasonStaleEvidence)
	}
	switch observed.DeletionTimestamp.State {
	case evidence.ValueReported:
		if observed.DeletionTimestamp.Value.After(*observed.Reference.CollectedAt.Value) {
			result.add(AssessmentInsufficient, ReasonInconsistentTime)
		} else {
			result.add(AssessmentBlocked, ReasonTargetDeleting)
		}
	case evidence.ValueInvalid:
		result.add(AssessmentInsufficient, ReasonMissingField)
	}
	if observed.Reference.ResourceVersion.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
	}
	if observed.Generation.State != evidence.ValueReported || *observed.Generation.Value < *plan.Generation.Value ||
		observed.Acknowledgement() != evidence.GenerationCurrent {
		result.add(AssessmentInsufficient, ReasonGeneration)
	}
	return result, nil
}

func canAssessConfiguration(result Assessment) bool {
	if result.State == AssessmentBlocked {
		return false
	}
	for _, reason := range result.Reasons {
		if reason != ReasonGeneration {
			return false
		}
	}
	return true
}

func configureAssessment(result *Assessment, actual evidence.Boolean, enabled bool) {
	if actual.State != evidence.ValueReported && actual.State != evidence.ValueImplied {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return
	}
	matches := *actual.Value == enabled
	result.Configured = reportedBoolean(matches)
	if !matches {
		result.OperatorObserved = reportedBoolean(false)
		result.add(AssessmentPending, ReasonNotConfigured)
	}
}

// AssessPause needs a later exact-target read and a current Available condition.
// Pausing requires Available=Unknown with reason Paused. Resuming requires an
// evaluated Available condition with a different reported reason. Availability
// may be False: resumed reconciliation does not establish cluster health.
func AssessPause(plan Plan, observed evidence.Cluster, policy AssessmentPolicy) (Assessment, error) {
	result, err := assessmentHeader(plan, observed, policy, ControlPause)
	if err != nil || !canAssessConfiguration(result) {
		return result, err
	}
	configureAssessment(&result, observed.Paused, plan.Enabled)
	if result.State != AssessmentObserved {
		return result, nil
	}
	if observed.Conditions.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return result, nil
	}
	for _, condition := range observed.Conditions.Values {
		if condition.Type != "Available" {
			continue
		}
		if condition.Acknowledgement(observed.Generation) != evidence.GenerationCurrent ||
			condition.Reason.State != evidence.ValueReported {
			result.add(AssessmentInsufficient, ReasonPauseCondition)
			return result, nil
		}
		if condition.LastTransitionAt.State == evidence.ValueInvalid || condition.LastTransitionAt.State == evidence.ValueReported &&
			condition.LastTransitionAt.Value.After(*observed.Reference.CollectedAt.Value) {
			result.add(AssessmentInsufficient, ReasonInconsistentTime)
			return result, nil
		}
		paused := *condition.Reason.Value == "Paused"
		matches := paused && condition.Status == evidence.ConditionUnknown
		if !plan.Enabled {
			matches = !paused && condition.Status != evidence.ConditionUnknown
		}
		result.OperatorObserved = reportedBoolean(matches)
		if !matches {
			result.add(AssessmentPending, ReasonPauseCondition)
		}
		return result, nil
	}
	result.add(AssessmentInsufficient, ReasonMissingField)
	return result, nil
}

// AssessMaintenance checks actual annotations on every confirmed workload in
// complete declared scopes. Disabling requires removal of the annotation. Caller
// associations and aggregate outcomes are ignored; associations are recomputed.
// A paused cluster blocks propagation. The function never resumes it. Pool and
// revision metadata group only UID-confirmed resources. Settled blue/green Idle
// uses its recorded Blue revision; active transitions remain insufficient.
// Final target verification must retain the original resource version because
// status-only topology changes do not necessarily change metadata.generation.
func AssessMaintenance(plan Plan, observed evidence.Cluster, related collect.RelatedResult,
	policy AssessmentPolicy,
) (Assessment, error) {
	result, err := assessmentHeader(plan, observed, policy, ControlMaintenance)
	if related.StatefulSets != nil {
		if !related.StatefulSets.Collection.Outcome.Valid() {
			return Assessment{}, ErrInvalidAssessment
		}
		result.StatefulSetRead = related.StatefulSets.Collection.Outcome
	}
	if related.Pods != nil {
		if !related.Pods.Collection.Outcome.Valid() {
			return Assessment{}, ErrInvalidAssessment
		}
		result.PodRead = related.Pods.Collection.Outcome
	}
	if err != nil || !canAssessConfiguration(result) {
		return result, err
	}
	configureAssessment(&result, observed.Maintenance.Enabled, plan.Enabled)
	if result.State != AssessmentObserved {
		return result, nil
	}
	if observed.Paused.State != evidence.ValueReported && observed.Paused.State != evidence.ValueImplied {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return result, nil
	}
	if *observed.Paused.Value {
		result.add(AssessmentBlocked, ReasonPaused)
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
	if len(related.StatefulSets.Observations) > policy.MaxCandidates ||
		len(related.Pods.Observations) > policy.MaxCandidates-len(related.StatefulSets.Observations) {
		return Assessment{}, ErrInvalidAssessment
	}
	sets, err := maintenanceSets(observed, related, policy, &result)
	if err != nil {
		return Assessment{}, err
	}
	if err := maintenancePods(observed, related, sets, policy, &result); err != nil {
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

func maintenanceVerification(cluster evidence.Cluster, verification evidence.TargetVerification, policy AssessmentPolicy) bool {
	return verification.Validate() == nil && verification.State == evidence.VerificationVerified &&
		verification.AdapterContract == cluster.AdapterContract && sameControlReference(verification.Before, cluster.Reference) &&
		sameControlInteger(verification.BeforeGeneration, cluster.Generation) && verification.After != nil &&
		verification.After.ResourceVersion.State == evidence.ValueReported &&
		*verification.After.ResourceVersion.Value == *cluster.Reference.ResourceVersion.Value &&
		!verification.Read.Interval.CompletedAt.After(policy.ReferenceTime) &&
		policy.ReferenceTime.Sub(*verification.After.CollectedAt.Value) <= policy.MaxEvidenceAge
}

func sameControlInteger(a, b evidence.Integer) bool {
	return a.State == b.State && (a.Value == nil && b.Value == nil || a.Value != nil && b.Value != nil && *a.Value == *b.Value)
}

func sameControlReference(a, b evidence.Reference) bool {
	return a.Identity == b.Identity && a.APIVersion == b.APIVersion && a.Field == b.Field &&
		a.ResourceVersion.State == b.ResourceVersion.State &&
		(a.ResourceVersion.Value == nil && b.ResourceVersion.Value == nil ||
			a.ResourceVersion.Value != nil && b.ResourceVersion.Value != nil && *a.ResourceVersion.Value == *b.ResourceVersion.Value) &&
		a.CollectedAt.State == b.CollectedAt.State && a.CollectedAt.Value != nil && b.CollectedAt.Value != nil &&
		a.CollectedAt.Value.Equal(*b.CollectedAt.Value)
}

type maintenanceSet struct {
	value       evidence.StatefulSet
	association evidence.Association
	pods        int64
}

func maintenanceScope(cluster evidence.Cluster, read evidence.Collection, count int, verification evidence.TargetVerification,
	result *Assessment,
) error {
	if read.Validate() != nil || read.Scope.Action != evidence.ReadList || read.Scope.Origin != cluster.Reference.Identity.Origin ||
		read.Scope.Namespace != cluster.Reference.Identity.Namespace || read.Items < count {
		return ErrInvalidAssessment
	}
	if read.Outcome == evidence.CollectionComplete && read.Items != count {
		return ErrInvalidAssessment
	}
	if read.Outcome != evidence.CollectionComplete {
		result.add(AssessmentInsufficient, ReasonReadIncomplete)
	}
	if read.Scope.Selector != "" && read.Scope.Selector != "openbao.org/cluster="+cluster.Reference.Identity.Name {
		result.add(AssessmentInsufficient, ReasonScopeRestricted)
	}
	if !read.Interval.StartedAt.IsZero() && (cluster.Reference.CollectedAt.Value.After(read.Interval.StartedAt) ||
		read.Interval.CompletedAt.After(verification.Read.Interval.StartedAt)) {
		result.add(AssessmentInsufficient, ReasonInconsistentTime)
	}
	return nil
}

func maintenanceReference(ref evidence.Reference, read evidence.Collection, contract, expected string,
	policy AssessmentPolicy, result *Assessment,
) error {
	if contract != expected || ref.Identity.Origin != read.Scope.Origin || ref.Identity.Namespace != read.Scope.Namespace ||
		ref.Identity.Group != read.Scope.Group {
		return ErrInvalidAssessment
	}
	if !read.Interval.Contains(*ref.CollectedAt.Value) || ref.CollectedAt.Value.After(policy.ReferenceTime) {
		result.add(AssessmentInsufficient, ReasonInconsistentTime)
	}
	if policy.ReferenceTime.Sub(*ref.CollectedAt.Value) > policy.MaxEvidenceAge {
		result.add(AssessmentInsufficient, ReasonStaleEvidence)
	}
	return nil
}

func maintenanceAnnotation(metadata evidence.WorkloadMetadata, enabled bool, result *Assessment) {
	if metadata.DeletionTimestamp.State == evidence.ValueInvalid || metadata.Maintenance.State == evidence.ValueInvalid {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return
	}
	if metadata.DeletionTimestamp.State == evidence.ValueReported {
		if metadata.DeletionTimestamp.Value.After(*metadata.Reference.CollectedAt.Value) {
			result.add(AssessmentInsufficient, ReasonInconsistentTime)
		} else {
			result.add(AssessmentPending, ReasonWorkloadDeleting)
		}
	}
	matches := metadata.Maintenance.State == evidence.ValueUnknown
	if enabled {
		matches = metadata.Maintenance.State == evidence.ValueReported && *metadata.Maintenance.Value == "true"
	}
	if !matches {
		result.add(AssessmentPending, ReasonAnnotationPending)
	}
}

func maintenanceSets(cluster evidence.Cluster, related collect.RelatedResult, policy AssessmentPolicy,
	result *Assessment,
) (map[string]*maintenanceSet, error) {
	read := related.StatefulSets.Collection
	values := related.StatefulSets.Observations
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
		if err := maintenanceReference(ref, read, set.AdapterContract, string(adapter.KubernetesAppsV1), policy, result); err != nil {
			return nil, err
		}
		association, err := evidence.AssociateStatefulSet(set, cluster.Reference.Identity)
		if err != nil {
			return nil, ErrInvalidAssessment
		}
		sets[ref.Identity.UID] = &maintenanceSet{value: set, association: association}
		switch association.State {
		case evidence.AssociationConfirmed:
			maintenanceAnnotation(set.Metadata, trueValue(cluster.Maintenance.Enabled), result)
			if set.DesiredReplicas.State != evidence.ValueReported || set.Acknowledgement() != evidence.GenerationCurrent {
				result.add(AssessmentInsufficient, ReasonGeneration)
			}
			maintenanceSetTopology(set, topology, result)
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

func trueValue(value evidence.Boolean) bool { return value.Value != nil && *value.Value }

func maintenancePods(cluster evidence.Cluster, related collect.RelatedResult, sets map[string]*maintenanceSet,
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
		if err := maintenanceReference(ref, read, pod.AdapterContract, string(adapter.KubernetesCoreV1), policy, result); err != nil {
			return err
		}
		if err := maintenancePod(pod, sets, trueValue(cluster.Maintenance.Enabled), result); err != nil {
			return err
		}
	}
	for _, set := range sets {
		if set.association.State == evidence.AssociationConfirmed && set.value.DesiredReplicas.State == evidence.ValueReported &&
			set.pods != *set.value.DesiredReplicas.Value {
			result.add(AssessmentPending, ReasonWorkloadMissing)
		}
	}
	return nil
}

func maintenancePod(pod evidence.Pod, sets map[string]*maintenanceSet, enabled bool, result *Assessment) error {
	for _, owner := range pod.Metadata.Owners.Values {
		if !owner.Controller || sets[owner.UID] == nil {
			continue
		}
		set := sets[owner.UID]
		association, err := evidence.AssociatePod(pod, set.value, set.association)
		if err != nil {
			return ErrInvalidAssessment
		}
		switch association.State {
		case evidence.AssociationConfirmed:
			maintenanceAnnotation(pod.Metadata, enabled, result)
			if maintenancePodTopology(pod, set.value, result) && pod.Metadata.DeletionTimestamp.State == evidence.ValueUnknown {
				set.pods++
			}
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
