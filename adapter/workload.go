package adapter

import (
	"slices"
	"strings"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func projectOwners(root map[string]any, limit int) evidence.Owners {
	value, state := field(root, "metadata", "ownerReferences")
	if state != evidence.ValueReported {
		return evidence.Owners{State: state}
	}
	items, ok := value.([]any)
	if !ok || len(items) > limit {
		return evidence.Owners{State: evidence.ValueInvalid}
	}
	values := []evidence.OwnerReference{}
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			return evidence.Owners{State: evidence.ValueInvalid}
		}
		controller := false
		if rawFlag, exists := raw["controller"]; exists && rawFlag != nil {
			flag, valid := rawFlag.(bool)
			if !valid {
				return evidence.Owners{State: evidence.ValueInvalid}
			}
			controller = flag
		}
		values = append(values, evidence.OwnerReference{
			APIVersion: required(raw, "apiVersion"), Kind: required(raw, "kind"),
			Name: required(raw, "name"), UID: required(raw, "uid"), Controller: controller,
		})
	}
	result, err := evidence.NewOwners(values)
	if err != nil {
		return evidence.Owners{State: evidence.ValueInvalid}
	}
	return result
}

func projectWorkloadMetadata(root map[string]any, ref evidence.Reference, limit int) evidence.WorkloadMetadata {
	maintenance := annotation(root, "openbao.org/maintenance")
	if maintenance.Value != nil && !slices.Contains([]string{"true", "false"}, *maintenance.Value) {
		maintenance = evidence.Text{State: evidence.ValueInvalid}
	}
	pool, revision := evidence.Text{State: evidence.ValueUnknown}, evidence.Text{State: evidence.ValueUnknown}
	if ref.Identity.Kind == "Pod" {
		pool = projectWorkloadPool(root, "metadata", "labels")
		revision = projectWorkloadRevision(root, "metadata", "labels")
	}
	return evidence.WorkloadMetadata{
		Reference: ref, Owners: projectOwners(root, limit),
		ClusterOwnerUID:   annotation(root, "openbao.org/owner-uid"),
		Maintenance:       maintenance,
		Pool:              pool,
		Revision:          revision,
		DeletionTimestamp: timestamp(root, "metadata", "deletionTimestamp"),
	}
}

func workloadLabel(root map[string]any, key string, path ...string) evidence.Text {
	labels, state := objectAt(root, path...)
	if state == evidence.ObjectInvalid || state == evidence.ObjectNull {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return text(labels, false, key)
}

func projectWorkloadPool(root map[string]any, path ...string) evidence.Text {
	pool := workloadLabel(root, "openbao.org/workload-pool", path...)
	if evidence.ValidateWorkloadPool(pool) != nil {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return pool
}

func projectWorkloadRevision(root map[string]any, path ...string) evidence.Text {
	revision := workloadLabel(root, "openbao.org/revision", path...)
	if evidence.ValidateWorkloadRevision(revision) != nil {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return revision
}

// ProjectStatefulSet projects apps/v1 facts under the explicit Kubernetes contract.
// Omitted readyReplicas is implied zero only after current positive acknowledgement
// with a known desired count, matching the pinned public omitempty declaration.
func ProjectStatefulSet(raw []byte, options ProjectionOptions) (evidence.StatefulSet, error) {
	root, ref, err := projectResource(raw, options, "StatefulSet")
	if err != nil {
		return evidence.StatefulSet{}, err
	}
	result := evidence.StatefulSet{
		Metadata: projectWorkloadMetadata(root, ref, options.Limits.MaxEntries), Generation: integer(root, "metadata", "generation"),
		ObservedGeneration: integer(root, "status", "observedGeneration"), DesiredReplicas: count(root, "spec", "replicas"),
		ReadyReplicas:    count(root, "status", "readyReplicas"),
		SelectorPool:     projectWorkloadPool(root, "spec", "selector", "matchLabels"),
		SelectorRevision: projectWorkloadRevision(root, "spec", "selector", "matchLabels"),
		TemplatePool:     projectWorkloadPool(root, "spec", "template", "metadata", "labels"),
		TemplateRevision: projectWorkloadRevision(root, "spec", "template", "metadata", "labels"),
	}
	if result.ReadyReplicas.State == evidence.ValueUnknown && result.DesiredReplicas.State == evidence.ValueReported &&
		result.Acknowledgement() == evidence.GenerationCurrent {
		zero := int64(0)
		result.ReadyReplicas = evidence.Integer{State: evidence.ValueImplied, Value: &zero}
	}
	if result.Validate() != nil {
		return evidence.StatefulSet{}, &ProjectionError{Code: InvalidInput}
	}
	return result, nil
}

func exitCode(root map[string]any) evidence.Integer {
	value := integer(root, "exitCode")
	if value.Value != nil && (*value.Value < -2147483648 || *value.Value > 2147483647) {
		return evidence.Integer{State: evidence.ValueInvalid}
	}
	return value
}

func projectContainers(root map[string]any, kind evidence.ContainerKind, limit int, path string) evidence.Containers {
	value, state := field(root, "status", path)
	if state != evidence.ValueReported {
		return evidence.Containers{State: state}
	}
	items, ok := value.([]any)
	if !ok || len(items) > limit {
		return evidence.Containers{State: evidence.ValueInvalid}
	}
	result := evidence.Containers{State: evidence.ValueReported, Values: []evidence.ContainerStatus{}}
	seen := map[string]bool{}
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			return evidence.Containers{State: evidence.ValueInvalid}
		}
		name := required(raw, "name")
		if name == "" || seen[name] {
			return evidence.Containers{State: evidence.ValueInvalid}
		}
		seen[name] = true
		status, statusState := objectAt(raw, "state")
		waiting, waitingState := childObject(statusState, status, "waiting")
		terminated, terminatedState := childObject(statusState, status, "terminated")
		result.Values = append(result.Values, evidence.ContainerStatus{
			Kind: kind, Name: name, Ready: boolean(raw, "ready"), RestartCount: count(raw, "restartCount"), State: statusState,
			Waiting: waitingState, WaitingReason: text(waiting, true, "reason"), Terminated: terminatedState,
			TerminatedReason: text(terminated, true, "reason"), ExitCode: exitCode(terminated),
		})
	}
	slices.SortFunc(result.Values, func(a, b evidence.ContainerStatus) int { return strings.Compare(a.Name, b.Name) })
	if result.Validate() != nil {
		return evidence.Containers{State: evidence.ValueInvalid}
	}
	return result
}

// ProjectPod excludes Pod specs, runtime messages, and arbitrary annotations.
// Init and application container-set presence remains independent.
func ProjectPod(raw []byte, options ProjectionOptions) (evidence.Pod, error) {
	root, ref, err := projectResource(raw, options, "Pod")
	if err != nil {
		return evidence.Pod{}, err
	}
	result := evidence.Pod{
		Metadata: projectWorkloadMetadata(root, ref, options.Limits.MaxEntries), Generation: integer(root, "metadata", "generation"),
		Phase:          enumText(root, []string{"Pending", "Running", "Succeeded", "Failed", "Unknown"}, "status", "phase"),
		Conditions:     projectConditions(root, options.Limits.MaxConditions, "status", "conditions"),
		InitContainers: projectContainers(root, evidence.ContainerInit, options.Limits.MaxEntries, "initContainerStatuses"),
		Containers:     projectContainers(root, evidence.ContainerApp, options.Limits.MaxEntries, "containerStatuses"),
	}
	if result.Validate() != nil {
		return evidence.Pod{}, &ProjectionError{Code: InvalidInput}
	}
	return result, nil
}
