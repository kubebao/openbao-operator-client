package diagnose

import (
	"maps"
	"slices"
	"strings"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func podReadiness(input ClusterInput, sets map[string]associatedSet, policy Policy, result *evaluation) {
	workloadScope(input.Pods.Collection, input.Subject, result)
	populationComplete := result.state == Evaluated && input.Collection.Outcome == evidence.CollectionComplete
	counts := map[string]int64{}
	values := slices.Clone(input.Pods.Observations)
	slices.SortFunc(values, func(a, b evidence.Pod) int {
		return strings.Compare(a.Metadata.Reference.Identity.UID, b.Metadata.Reference.Identity.UID)
	})
	for _, pod := range values {
		if !fresh(pod.Metadata.Reference, input.Pods.Collection, policy) {
			result.insufficient(ReasonStaleEvidence)
			populationComplete = false
			continue
		}
		parent, confirmed := podParent(pod, sets)
		if !confirmed {
			result.insufficient(ReasonAssociation)
			populationComplete = false
			continue
		}
		if deleting(pod.Metadata, result) {
			if pod.Metadata.DeletionTimestamp.State == evidence.ValueInvalid ||
				pod.Metadata.DeletionTimestamp.Value.After(*pod.Metadata.Reference.CollectedAt.Value) {
				populationComplete = false
			}
			continue
		}
		counts[parent]++
		evaluatePod(pod, result)
	}
	// Population needs both complete scopes, resolved associations, current
	// replica intent, and stable target verification. A positive Pod finding
	// remains usable independently when a different candidate is incomplete.
	if populationComplete {
		podPopulation(sets, counts, result)
	}
}

func podParent(pod evidence.Pod, sets map[string]associatedSet) (string, bool) {
	for _, owner := range pod.Metadata.Owners.Values {
		set, exists := sets[owner.UID]
		if !owner.Controller || !exists {
			continue
		}
		association, err := evidence.AssociatePod(pod, set.value, set.association)
		if err != nil || association.State != evidence.AssociationConfirmed {
			return "", false
		}
		return owner.UID, true
	}
	return "", false
}

func evaluatePod(pod evidence.Pod, result *evaluation) {
	ref := pod.Metadata.Reference
	failed := pod.Phase.State == evidence.ValueReported && *pod.Phase.Value == "Failed"
	if pod.Phase.State != evidence.ValueReported || *pod.Phase.Value == "Unknown" ||
		!slices.Contains([]string{"Pending", "Running", "Succeeded", "Failed"}, *pod.Phase.Value) {
		result.insufficient(ReasonMissingField)
	}
	if failed {
		result.add(resourceFinding(ref, RulePod, "phase", Error, "The UID-associated Pod reports Failed phase.", "status.phase"))
	}
	if pod.Conditions.State != evidence.ValueReported {
		result.insufficient(ReasonMissingField)
		return
	}
	for _, condition := range pod.Conditions.Values {
		if condition.Type != "Ready" {
			continue
		}
		if condition.Acknowledgement(pod.Generation) != evidence.GenerationCurrent {
			result.insufficient(ReasonGeneration)
			return
		}
		if condition.Status == evidence.ConditionUnknown {
			result.insufficient(ReasonMissingField)
			return
		}
		if condition.LastTransitionAt.State == evidence.ValueInvalid ||
			condition.LastTransitionAt.State == evidence.ValueReported && condition.LastTransitionAt.Value.After(*ref.CollectedAt.Value) {
			result.insufficient(ReasonTime)
		}
		if condition.Status == evidence.ConditionFalse && !failed {
			found := resourceFinding(
				ref,
				RulePod,
				"ready",
				Warning,
				"The UID-associated Pod reports Ready=False for its current generation.",
				"metadata.generation",
				"status.conditions[type=Ready]",
			)
			waitingReferences(found, pod)
			result.add(found)
		}
		return
	}
	result.insufficient(ReasonMissingField)
}

func waitingReferences(found *Finding, pod evidence.Pod) {
	for _, containers := range []evidence.Containers{pod.InitContainers, pod.Containers} {
		for _, container := range containers.Values {
			if container.WaitingReason.State != evidence.ValueReported {
				continue
			}
			field := "status.containerStatuses"
			if container.Kind == evidence.ContainerInit {
				field = "status.initContainerStatuses"
			}
			ref := pod.Metadata.Reference
			ref.Field = field + "[name=" + container.Name + "].state.waiting"
			found.References = append(found.References, ref)
		}
	}
}

func podPopulation(sets map[string]associatedSet, counts map[string]int64, result *evaluation) {
	for _, uid := range slices.Sorted(maps.Keys(sets)) {
		set := sets[uid]
		value := set.value
		if set.association.State != evidence.AssociationConfirmed || deleting(value.Metadata, result) {
			continue
		}
		if value.Acknowledgement() != evidence.GenerationCurrent || !knownInteger(value.DesiredReplicas) {
			result.insufficient(ReasonGeneration)
			continue
		}
		if counts[uid] < *value.DesiredReplicas.Value {
			result.add(resourceFinding(value.Metadata.Reference, RulePod, "pod-population", Warning,
				"Fewer nonterminating UID-associated Pods were captured than desired in the declared workload scope.",
				"metadata.generation", "status.observedGeneration", "spec.replicas"))
		}
	}
}
