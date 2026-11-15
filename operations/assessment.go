package operations

import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// ErrInvalidAssessment reports invalid assessment input: a plan, observation, policy, or related read.
var ErrInvalidAssessment = errors.New("invalid control assessment")

const (
	// defaultMaxEvidenceAge bounds the age of supplied evidence at the reference time.
	defaultMaxEvidenceAge = 5 * time.Minute
	// defaultMaxCandidates bounds the related StatefulSets and Pods in one assessment.
	defaultMaxCandidates = 5000
)

// AssessmentState summarizes an assessment. States are ordered by severity:
// observed, pending, blocked, insufficient.
type AssessmentState string

const (
	// AssessmentObserved means the supplied evidence shows the requested state.
	AssessmentObserved AssessmentState = "observed"
	// AssessmentPending means the evidence does not yet show the requested state.
	AssessmentPending AssessmentState = "pending"
	// AssessmentBlocked means the evidence shows a state that prevents the request from taking effect.
	AssessmentBlocked AssessmentState = "blocked"
	// AssessmentInsufficient means the evidence cannot establish the state.
	AssessmentInsufficient AssessmentState = "insufficient"
)

// rank orders assessment states by severity.
func (s AssessmentState) rank() int {
	switch s {
	case AssessmentObserved:
		return 0
	case AssessmentPending:
		return 1
	case AssessmentBlocked:
		return 2
	case AssessmentInsufficient:
		return 3
	default:
		return 0
	}
}

// AssessmentReason explains an assessment state.
type AssessmentReason string

const (
	// ReasonTargetChanged means the observation identifies another resource or API version than the plan.
	ReasonTargetChanged AssessmentReason = "target_changed"
	// ReasonTargetDeleting means the target has a deletion timestamp.
	ReasonTargetDeleting AssessmentReason = "target_deleting"
	// ReasonStaleEvidence means evidence exceeds the policy's maximum age.
	ReasonStaleEvidence AssessmentReason = "stale_evidence"
	// ReasonInconsistentTime means timestamps contradict the read order or reference time.
	ReasonInconsistentTime AssessmentReason = "inconsistent_time"
	// ReasonMissingField means a required field was not reported.
	ReasonMissingField AssessmentReason = "missing_field"
	// ReasonInvalidField means a required field was reported with an invalid value.
	ReasonInvalidField AssessmentReason = "invalid_field"
	// ReasonGeneration means a producer has not acknowledged the current generation.
	ReasonGeneration AssessmentReason = "generation_unconfirmed"
	// ReasonNotConfigured means the observed spec does not contain the planned value.
	ReasonNotConfigured AssessmentReason = "not_configured"
	// ReasonPauseCondition means the Available condition does not confirm the planned pause state.
	ReasonPauseCondition AssessmentReason = "pause_condition_unconfirmed"
	// ReasonPaused means a paused cluster cannot propagate maintenance to its workloads.
	ReasonPaused AssessmentReason = "paused"
	// ReasonReadNotRequested means a required related read was not supplied.
	ReasonReadNotRequested AssessmentReason = "read_not_requested"
	// ReasonReadIncomplete means a related read did not complete.
	ReasonReadIncomplete AssessmentReason = "read_incomplete"
	// ReasonScopeRestricted means a related read used a narrower selector than the cluster's workloads.
	ReasonScopeRestricted AssessmentReason = "scope_restricted"
	// ReasonTargetUnverified means the final target verification does not confirm the observed revision.
	ReasonTargetUnverified AssessmentReason = "target_unverified"
	// ReasonAssociation means a workload's association with the cluster is unconfirmed.
	ReasonAssociation AssessmentReason = "association_unconfirmed"
	// ReasonAnnotationPending means a workload does not yet carry the planned maintenance annotation.
	ReasonAnnotationPending AssessmentReason = "annotation_pending"
	// ReasonWorkloadMissing means an expected workload or Pod was not observed.
	ReasonWorkloadMissing AssessmentReason = "workload_missing"
	// ReasonWorkloadDeleting means a workload has a deletion timestamp.
	ReasonWorkloadDeleting AssessmentReason = "workload_deleting"
	// ReasonTopologyUnconfirmed means an active blue/green transition prevents a topology comparison.
	ReasonTopologyUnconfirmed AssessmentReason = "topology_unconfirmed"
	// ReasonTopologyConflict means workload pool or revision labels contradict the cluster topology.
	ReasonTopologyConflict AssessmentReason = "topology_conflict"
	// ReasonTopologyAmbiguous means several workloads claim the same pool.
	ReasonTopologyAmbiguous AssessmentReason = "topology_ambiguous"
)

// AssessmentPolicy supplies evaluation time and bounds. Assessment reads no clock.
type AssessmentPolicy struct {
	ReferenceTime  time.Time     `json:"referenceTime"`
	MaxEvidenceAge time.Duration `json:"maxEvidenceAge"`
	MaxCandidates  int           `json:"maxCandidates"`
}

// DefaultAssessmentPolicy returns a policy at reference with a five-minute
// evidence age limit and at most 5000 related candidates.
func DefaultAssessmentPolicy(reference time.Time) AssessmentPolicy {
	return AssessmentPolicy{
		ReferenceTime:  reference,
		MaxEvidenceAge: defaultMaxEvidenceAge,
		MaxCandidates:  defaultMaxCandidates,
	}
}

// validate checks a valid reference time and positive bounds.
func (p AssessmentPolicy) validate() error {
	at := p.ReferenceTime
	if at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return invalidAssessment("policy.referenceTime", nil)
	}
	if p.MaxEvidenceAge <= 0 {
		return invalidAssessment("policy.maxEvidenceAge", nil)
	}
	if p.MaxCandidates <= 0 {
		return invalidAssessment("policy.maxCandidates", nil)
	}
	return nil
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
		State:            AssessmentObserved,
		Configured:       evidence.Boolean{State: evidence.ValueUnknown},
		OperatorObserved: evidence.Boolean{State: evidence.ValueUnknown},
		Reasons:          []AssessmentReason{},
		StatefulSetRead:  evidence.CollectionNotRequested,
		PodRead:          evidence.CollectionNotRequested,
	}
}

// add records a reason and raises the state to the more severe of the two.
func (a *Assessment) add(state AssessmentState, reason AssessmentReason) {
	if state.rank() > a.State.rank() {
		a.State = state
	}
	a.Reasons = append(a.Reasons, reason)
	slices.Sort(a.Reasons)
	a.Reasons = slices.Compact(a.Reasons)
}

// invalidAssessment attributes ErrInvalidAssessment, and an optional cause, to a field path.
func invalidAssessment(path string, cause error) error {
	return evidence.At(path, errors.Join(ErrInvalidAssessment, cause))
}

func reportedBoolean(value bool) evidence.Boolean {
	return evidence.Boolean{State: evidence.ValueReported, Value: &value}
}

func assessmentHeader(plan Plan, observed evidence.Cluster, policy AssessmentPolicy, control Control) (Assessment, error) {
	if err := plan.Validate(); err != nil {
		return Assessment{}, invalidAssessment("plan", err)
	}
	if plan.Control != control {
		return Assessment{}, invalidAssessment("plan.control", nil)
	}
	return assessmentTarget(plan.Target, plan.Generation, observed, policy)
}

// assessmentTarget compares a later observation with the planned target and generation.
func assessmentTarget(
	target evidence.Reference,
	generation evidence.Integer,
	observed evidence.Cluster,
	policy AssessmentPolicy,
) (Assessment, error) {
	if err := observed.Validate(); err != nil {
		return Assessment{}, invalidAssessment("observed", err)
	}
	if err := policy.validate(); err != nil {
		return Assessment{}, err
	}
	result := newAssessment()
	at := policy.ReferenceTime
	ref := observed.Reference
	if ref.Identity != target.Identity || ref.APIVersion != target.APIVersion {
		result.add(AssessmentInsufficient, ReasonTargetChanged)
		return result, nil
	}
	collectedAt := *ref.CollectedAt.Value
	if !collectedAt.After(*target.CollectedAt.Value) || collectedAt.After(at) {
		result.add(AssessmentInsufficient, ReasonInconsistentTime)
	}
	if at.Sub(collectedAt) > policy.MaxEvidenceAge {
		result.add(AssessmentInsufficient, ReasonStaleEvidence)
	}
	switch observed.DeletionTimestamp.State {
	case evidence.ValueReported:
		if observed.DeletionTimestamp.Value.After(collectedAt) {
			result.add(AssessmentInsufficient, ReasonInconsistentTime)
		} else {
			result.add(AssessmentBlocked, ReasonTargetDeleting)
		}
	case evidence.ValueInvalid:
		result.add(AssessmentInsufficient, ReasonMissingField)
	}
	if ref.ResourceVersion.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonMissingField)
	}
	if observed.Generation.State != evidence.ValueReported ||
		*observed.Generation.Value < *generation.Value ||
		observed.Acknowledgement() != evidence.GenerationCurrent {
		result.add(AssessmentInsufficient, ReasonGeneration)
	}
	return result, nil
}

// canAssessConfiguration reports whether only an unacknowledged generation, if
// anything, limits the target comparison.
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
	if !knownBoolean(actual) {
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
	index := slices.IndexFunc(observed.Conditions.Values, func(condition evidence.Condition) bool {
		return condition.Type == "Available"
	})
	if index < 0 {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return result, nil
	}
	condition := observed.Conditions.Values[index]
	if condition.Acknowledgement(observed.Generation) != evidence.GenerationCurrent ||
		condition.Reason.State != evidence.ValueReported {
		result.add(AssessmentInsufficient, ReasonPauseCondition)
		return result, nil
	}
	if transitionAfter(condition, *observed.Reference.CollectedAt.Value) {
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

// transitionAfter reports an invalid transition time or one after the read time.
func transitionAfter(condition evidence.Condition, collectedAt time.Time) bool {
	switch condition.LastTransitionAt.State {
	case evidence.ValueInvalid:
		return true
	case evidence.ValueReported:
		return condition.LastTransitionAt.Value.After(collectedAt)
	default:
		return false
	}
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
			return Assessment{}, invalidAssessment("related.statefulSets.collection.outcome", nil)
		}
		result.StatefulSetRead = related.StatefulSets.Collection.Outcome
	}
	if related.Pods != nil {
		if !related.Pods.Collection.Outcome.Valid() {
			return Assessment{}, invalidAssessment("related.pods.collection.outcome", nil)
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
	if !knownBoolean(observed.Paused) {
		result.add(AssessmentInsufficient, ReasonMissingField)
		return result, nil
	}
	if *observed.Paused.Value {
		result.add(AssessmentBlocked, ReasonPaused)
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
	enabled := trueValue(observed.Maintenance.Enabled)
	checks := workloadChecks{
		set: func(set evidence.StatefulSet) {
			maintenanceAnnotation(set.Metadata, enabled, &result)
			if set.DesiredReplicas.State != evidence.ValueReported || set.Acknowledgement() != evidence.GenerationCurrent {
				result.add(AssessmentInsufficient, ReasonGeneration)
			}
		},
		pod: func(pod evidence.Pod, _ evidence.StatefulSet) {
			maintenanceAnnotation(pod.Metadata, enabled, &result)
		},
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

// targetVerified reports whether a valid final verification confirms the
// observed cluster's reference, generation, and resource version within policy.
func targetVerified(cluster evidence.Cluster, verification evidence.TargetVerification, policy AssessmentPolicy) bool {
	if verification.Validate() != nil || verification.State != evidence.VerificationVerified {
		return false
	}
	if !sameControlReference(verification.Before, cluster.Reference) ||
		!sameControlInteger(verification.BeforeGeneration, cluster.Generation) {
		return false
	}
	after := verification.After
	if after == nil || after.ResourceVersion.State != evidence.ValueReported ||
		*after.ResourceVersion.Value != *cluster.Reference.ResourceVersion.Value {
		return false
	}
	return !verification.Read.Interval.CompletedAt.After(policy.ReferenceTime) &&
		policy.ReferenceTime.Sub(*after.CollectedAt.Value) <= policy.MaxEvidenceAge
}

// equalPointers reports whether two optional values are both absent or both present and equal.
func equalPointers[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameControlInteger(a, b evidence.Integer) bool {
	return a.State == b.State && equalPointers(a.Value, b.Value)
}

func sameControlText(a, b evidence.Text) bool {
	return a.State == b.State && equalPointers(a.Value, b.Value)
}

func sameControlReference(a, b evidence.Reference) bool {
	if a.Identity != b.Identity || a.APIVersion != b.APIVersion || a.Field != b.Field {
		return false
	}
	if !sameControlText(a.ResourceVersion, b.ResourceVersion) || a.CollectedAt.State != b.CollectedAt.State {
		return false
	}
	return a.CollectedAt.Value != nil && b.CollectedAt.Value != nil && a.CollectedAt.Value.Equal(*b.CollectedAt.Value)
}

func trueValue(value evidence.Boolean) bool { return value.Value != nil && *value.Value }

// maintenanceAnnotation compares a workload's maintenance annotation with the planned value.
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

// workloadChecks applies control-specific checks to UID-confirmed workloads.
// Topology, association, and Pod counting are shared by every control.
type workloadChecks struct {
	set func(evidence.StatefulSet)
	pod func(evidence.Pod, evidence.StatefulSet)
}

type relatedSet struct {
	value       evidence.StatefulSet
	association evidence.Association
	pods        int64
}

// assessWorkloads checks the related StatefulSet and Pod reads for one cluster.
func assessWorkloads(
	cluster evidence.Cluster,
	related collect.RelatedResult,
	policy AssessmentPolicy,
	result *Assessment,
	checks workloadChecks,
) error {
	sets, pods := len(related.StatefulSets.Observations), len(related.Pods.Observations)
	if sets > policy.MaxCandidates {
		return invalidAssessment("related.statefulSets.observations", nil)
	}
	if pods > policy.MaxCandidates-sets {
		return invalidAssessment("related.pods.observations", nil)
	}
	confirmed, err := assessStatefulSets(cluster, related, policy, result, checks.set)
	if err != nil {
		return err
	}
	return assessPods(cluster, related, confirmed, policy, result, checks.pod)
}

func observationPath(read string, index int) string {
	return read + ".observations[" + strconv.Itoa(index) + "]"
}

// checkRelatedRead checks a related LIST's declared scope and item count, and
// records an incomplete, restricted, or misordered read.
func checkRelatedRead(
	path string,
	cluster evidence.Cluster,
	read evidence.Collection,
	count int,
	verification evidence.TargetVerification,
	result *Assessment,
) error {
	if err := read.Validate(); err != nil {
		return invalidAssessment(path+".collection", err)
	}
	id := cluster.Reference.Identity
	if read.Scope.Action != evidence.ReadList || read.Scope.Origin != id.Origin || read.Scope.Namespace != id.Namespace {
		return invalidAssessment(path+".collection.scope", nil)
	}
	if read.Items < count || read.Outcome == evidence.CollectionComplete && read.Items != count {
		return invalidAssessment(path+".collection.items", nil)
	}
	if read.Outcome != evidence.CollectionComplete {
		result.add(AssessmentInsufficient, ReasonReadIncomplete)
	}
	if read.Scope.Selector != "" && read.Scope.Selector != "openbao.org/cluster="+id.Name {
		result.add(AssessmentInsufficient, ReasonScopeRestricted)
	}
	started := read.Interval.StartedAt
	if !started.IsZero() && (cluster.Reference.CollectedAt.Value.After(started) ||
		read.Interval.CompletedAt.After(verification.Read.Interval.StartedAt)) {
		result.add(AssessmentInsufficient, ReasonInconsistentTime)
	}
	return nil
}

// checkRelatedReference checks that a related observation belongs to its read
// scope, and records a misordered or stale read time.
func checkRelatedReference(
	path string,
	ref evidence.Reference,
	read evidence.Collection,
	policy AssessmentPolicy,
	result *Assessment,
) error {
	version := read.Scope.Version
	if read.Scope.Group != "" {
		version = read.Scope.Group + "/" + version
	}
	if ref.APIVersion != version {
		return invalidAssessment(path+".apiVersion", nil)
	}
	if ref.Identity.Origin != read.Scope.Origin ||
		ref.Identity.Namespace != read.Scope.Namespace ||
		ref.Identity.Group != read.Scope.Group {
		return invalidAssessment(path+".identity", nil)
	}
	collectedAt := *ref.CollectedAt.Value
	if !read.Interval.Contains(collectedAt) || collectedAt.After(policy.ReferenceTime) {
		result.add(AssessmentInsufficient, ReasonInconsistentTime)
	}
	if policy.ReferenceTime.Sub(collectedAt) > policy.MaxEvidenceAge {
		result.add(AssessmentInsufficient, ReasonStaleEvidence)
	}
	return nil
}

// assessStatefulSets associates every related StatefulSet with the cluster and
// checks confirmed sets against the topology and control-specific checks.
func assessStatefulSets(
	cluster evidence.Cluster,
	related collect.RelatedResult,
	policy AssessmentPolicy,
	result *Assessment,
	check func(evidence.StatefulSet),
) (map[string]*relatedSet, error) {
	const path = "related.statefulSets"
	read := related.StatefulSets.Collection
	values := related.StatefulSets.Observations
	if read.Scope.Group != "apps" || read.Scope.Version != "v1" || read.Scope.Resource != "statefulsets" {
		return nil, invalidAssessment(path+".collection.scope", nil)
	}
	if values == nil && read.Outcome == evidence.CollectionComplete {
		return nil, invalidAssessment(path+".observations", nil)
	}
	if err := checkRelatedRead(path, cluster, read, len(values), related.Verification, result); err != nil {
		return nil, err
	}
	sets := map[string]*relatedSet{}
	names := map[string]bool{}
	topology := maintenanceTopology(cluster, result)
	for index, set := range values {
		itemPath := observationPath(path, index)
		if err := set.Validate(); err != nil {
			return nil, invalidAssessment(itemPath, err)
		}
		ref := set.Metadata.Reference
		if sets[ref.Identity.UID] != nil || names[ref.Identity.Name] {
			return nil, invalidAssessment(itemPath+".metadata.reference.identity", nil)
		}
		names[ref.Identity.Name] = true
		if err := checkRelatedReference(itemPath+".metadata.reference", ref, read, policy, result); err != nil {
			return nil, err
		}
		association, err := evidence.AssociateStatefulSet(set, cluster.Reference.Identity)
		if err != nil {
			return nil, invalidAssessment(itemPath, err)
		}
		sets[ref.Identity.UID] = &relatedSet{value: set, association: association}
		switch association.State {
		case evidence.AssociationConfirmed:
			maintenanceSetTopology(set, topology, result)
			check(set)
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

// assessPods associates every related Pod through its controlling StatefulSet,
// counts confirmed Pods per set, and applies control-specific checks.
func assessPods(
	cluster evidence.Cluster,
	related collect.RelatedResult,
	sets map[string]*relatedSet,
	policy AssessmentPolicy,
	result *Assessment,
	check func(evidence.Pod, evidence.StatefulSet),
) error {
	const path = "related.pods"
	read := related.Pods.Collection
	values := related.Pods.Observations
	if read.Scope.Group != "" || read.Scope.Version != "v1" || read.Scope.Resource != "pods" {
		return invalidAssessment(path+".collection.scope", nil)
	}
	if values == nil && read.Outcome == evidence.CollectionComplete {
		return invalidAssessment(path+".observations", nil)
	}
	if err := checkRelatedRead(path, cluster, read, len(values), related.Verification, result); err != nil {
		return err
	}
	uids := map[string]bool{}
	names := map[string]bool{}
	for index, pod := range values {
		itemPath := observationPath(path, index)
		if err := pod.Validate(); err != nil {
			return invalidAssessment(itemPath, err)
		}
		ref := pod.Metadata.Reference
		if uids[ref.Identity.UID] || names[ref.Identity.Name] {
			return invalidAssessment(itemPath+".metadata.reference.identity", nil)
		}
		uids[ref.Identity.UID] = true
		names[ref.Identity.Name] = true
		if err := checkRelatedReference(itemPath+".metadata.reference", ref, read, policy, result); err != nil {
			return err
		}
		if err := assessPod(itemPath, pod, sets, result, check); err != nil {
			return err
		}
	}
	for _, set := range sets {
		desired := set.value.DesiredReplicas
		if set.association.State == evidence.AssociationConfirmed &&
			desired.State == evidence.ValueReported &&
			set.pods != *desired.Value {
			result.add(AssessmentPending, ReasonWorkloadMissing)
		}
	}
	return nil
}

// assessPod checks one Pod against the first related StatefulSet that controls it.
// A Pod without such a controller must prove that another kind controls it.
func assessPod(
	path string,
	pod evidence.Pod,
	sets map[string]*relatedSet,
	result *Assessment,
	check func(evidence.Pod, evidence.StatefulSet),
) error {
	for _, owner := range pod.Metadata.Owners.Values {
		set := sets[owner.UID]
		if !owner.Controller || set == nil {
			continue
		}
		association, err := evidence.AssociatePod(pod, set.value, set.association)
		if err != nil {
			return invalidAssessment(path, err)
		}
		switch association.State {
		case evidence.AssociationConfirmed:
			counted := maintenancePodTopology(pod, set.value, result)
			if counted && pod.Metadata.DeletionTimestamp.State == evidence.ValueUnknown {
				set.pods++
			}
			check(pod, set.value)
		case evidence.AssociationForeign:
		default:
			result.add(AssessmentInsufficient, ReasonAssociation)
		}
		return nil
	}
	excluded, err := evidence.PodHasNonStatefulSetController(pod)
	if err != nil {
		return invalidAssessment(path, err)
	}
	if !excluded {
		result.add(AssessmentInsufficient, ReasonAssociation)
	}
	return nil
}
