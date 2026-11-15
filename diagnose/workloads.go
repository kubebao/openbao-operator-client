package diagnose

import (
	"maps"
	"slices"
	"strings"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type associatedSet struct {
	value       evidence.StatefulSet
	association evidence.Association
}

func evaluateWorkloads(input ClusterInput, rule RuleID, policy Policy, coverage Coverage) (Coverage, []Finding) {
	var read *evidence.Collection
	if rule == RuleWorkload && input.StatefulSets != nil {
		read = &input.StatefulSets.Collection
	}
	if rule == RulePod && input.Pods != nil {
		read = &input.Pods.Collection
	}
	coverage.State = CoverageNotRequested
	coverage.Reason = ReasonNotRequested
	coverage.Outcome = evidence.CollectionNotRequested
	if read == nil {
		return coverage, nil
	}
	coverage.Outcome = read.Outcome
	if read.Outcome == evidence.CollectionNotRequested {
		return coverage, nil
	}
	coverage.State = CoverageInsufficient
	coverage.Reason = ReasonMissingObservation
	if reason := relatedTarget(input, policy); reason != ReasonNone {
		coverage.Reason = reason
		return coverage, nil
	}
	result := evaluation{state: CoverageEvaluated, reason: ReasonNone}
	if input.Collection.Outcome != evidence.CollectionComplete {
		result.insufficient(ReasonScopeIncomplete)
	}
	sets := associatedSets(input, policy, &result)
	if rule == RuleWorkload {
		workloadReadiness(input, sets, &result)
	} else {
		podReadiness(input, sets, policy, &result)
	}
	if input.Collection.Outcome != evidence.CollectionComplete {
		result.insufficient(ReasonScopeIncomplete)
	}
	return result.apply(coverage)
}

func relatedTarget(input ClusterInput, policy Policy) CoverageReason {
	if input.Observation == nil {
		return ReasonMissingObservation
	}
	if input.Collection.Outcome == evidence.CollectionDeleted || input.Collection.Outcome == evidence.CollectionReplaced {
		return ReasonVerification
	}
	if input.Verification == nil || input.Verification.State != evidence.VerificationVerified {
		return ReasonVerification
	}
	if !fresh(input.Observation.Reference, input.Collection, policy) {
		return ReasonStaleEvidence
	}
	return ReasonNone
}

// fresh reports whether a reference was collected within its read, not after the
// reference time, and within the policy's evidence age limit.
func fresh(ref evidence.Reference, read evidence.Collection, policy Policy) bool {
	collected := *ref.CollectedAt.Value
	if !read.Interval.Contains(collected) || collected.After(policy.ReferenceTime) {
		return false
	}
	return policy.ReferenceTime.Sub(collected) <= policy.MaxEvidenceAge
}

// These are the existing CLI workload scopes. A narrower query can support
// individual observations, but cannot support population or absence findings.
func workloadScope(read evidence.Collection, target evidence.Identity, result *evaluation) {
	complete := read.Outcome == evidence.CollectionComplete
	if !complete {
		result.insufficient(ReasonScopeIncomplete)
	}
	if read.Scope.Selector != "" && read.Scope.Selector != "openbao.org/cluster="+target.Name {
		result.insufficient(ReasonScopeRestricted)
	}
}

func deleting(metadata evidence.WorkloadMetadata, result *evaluation) bool {
	if metadata.DeletionTimestamp.State == evidence.ValueInvalid {
		result.insufficient(ReasonMissingField)
		return true
	}
	if metadata.DeletionTimestamp.State == evidence.ValueReported {
		if metadata.DeletionTimestamp.Value.After(*metadata.Reference.CollectedAt.Value) {
			result.insufficient(ReasonTime)
		}
		return true
	}
	return false
}

func associatedSets(input ClusterInput, policy Policy, result *evaluation) map[string]associatedSet {
	sets := map[string]associatedSet{}
	if input.StatefulSets == nil {
		result.insufficient(ReasonMissingObservation)
		return sets
	}
	workloadScope(input.StatefulSets.Collection, input.Subject, result)
	values := slices.Clone(input.StatefulSets.Observations)
	slices.SortFunc(values, func(a, b evidence.StatefulSet) int {
		return strings.Compare(a.Metadata.Reference.Identity.UID, b.Metadata.Reference.Identity.UID)
	})
	for _, set := range values {
		if !fresh(set.Metadata.Reference, input.StatefulSets.Collection, policy) {
			result.insufficient(ReasonStaleEvidence)
			continue
		}
		// Input validation has checked the normalized shapes and target identity.
		association, err := evidence.AssociateStatefulSet(set, input.Subject)
		if err != nil {
			result.insufficient(ReasonAssociation)
			continue
		}
		if association.State != evidence.AssociationConfirmed && association.State != evidence.AssociationForeign {
			result.insufficient(ReasonAssociation)
		}
		sets[set.Metadata.Reference.Identity.UID] = associatedSet{value: set, association: association}
	}
	return sets
}

func workloadReadiness(input ClusterInput, sets map[string]associatedSet, result *evaluation) {
	confirmed := 0
	for _, uid := range slices.Sorted(maps.Keys(sets)) {
		set := sets[uid]
		if set.association.State != evidence.AssociationConfirmed {
			continue
		}
		confirmed++
		value := set.value
		if deleting(value.Metadata, result) {
			continue
		}
		if value.Acknowledgement() != evidence.GenerationCurrent {
			result.insufficient(ReasonGeneration)
			continue
		}
		if !knownInteger(value.DesiredReplicas) || !knownInteger(value.ReadyReplicas) {
			result.insufficient(ReasonMissingField)
			continue
		}
		if *value.ReadyReplicas.Value < *value.DesiredReplicas.Value {
			result.add(resourceFinding(value.Metadata.Reference, RuleWorkload, "ready", SeverityWarning,
				"The UID-associated StatefulSet reports fewer ready replicas than desired.",
				"metadata.generation", "status.observedGeneration", "spec.replicas", "status.readyReplicas"))
		}
	}
	if confirmed != 0 || result.state != CoverageEvaluated {
		return
	}
	cluster := input.Observation
	if !knownInteger(cluster.DesiredReplicas) || cluster.Acknowledgement() != evidence.GenerationCurrent {
		result.insufficient(ReasonMissingField)
		return
	}
	if *cluster.DesiredReplicas.Value > 0 {
		found := finding(*cluster, RuleWorkload, SeverityWarning,
			"No UID-associated StatefulSet was found in the declared workload scope.", "spec.replicas")
		found.Subsubject = "statefulset-population"
		result.add(found)
	}
}

func resourceFinding(ref evidence.Reference, rule RuleID, subsubject string, severity Severity, summary string,
	fields ...string,
) *Finding {
	found := &Finding{
		Rule:       rule,
		Subject:    ref.Identity,
		Subsubject: subsubject,
		Severity:   severity,
		Summary:    summary,
		References: []evidence.Reference{},
	}
	for _, field := range fields {
		value := ref
		value.Field = field
		found.References = append(found.References, value)
	}
	return found
}
