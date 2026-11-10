package adapter

import (
	"slices"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// ProjectionOptions binds one read to a caller-supplied origin, time, and known contract.
type ProjectionOptions struct {
	Origin      string
	CollectedAt time.Time
	Contract    Contract
	Limits      ProjectionLimits
}

// ProjectCluster validates and projects approved OpenBaoCluster fields. Invalid optional
// fields remain distinguishable without discarding independent facts. Raw data and provider
// messages are never retained. The function performs no reads and never uses the wall clock.
func ProjectCluster(raw []byte, options ProjectionOptions) (evidence.Cluster, error) {
	info, err := Describe(options.Contract)
	if err != nil {
		return evidence.Cluster{}, err
	}
	if options.CollectedAt.IsZero() {
		return evidence.Cluster{}, &ProjectionError{Code: InvalidInput}
	}
	object, err := decodeObject(raw, options.Limits)
	if err != nil {
		return evidence.Cluster{}, err
	}
	if required(object, "apiVersion") != info.APIVersion || required(object, "kind") != "OpenBaoCluster" {
		return evidence.Cluster{}, &ProjectionError{Code: ResourceMismatch}
	}
	identity := evidence.Identity{
		Origin: options.Origin, Group: info.APIGroup, Kind: "OpenBaoCluster",
		Namespace: required(object, "metadata", "namespace"), Name: required(object, "metadata", "name"),
		UID: required(object, "metadata", "uid"),
	}
	if identity.Validate() != nil {
		return evidence.Cluster{}, &ProjectionError{Code: InvalidInput}
	}
	collected := options.CollectedAt.UTC()
	result := evidence.Cluster{
		SchemaVersion: evidence.SchemaVersion, AdapterContract: string(options.Contract),
		Reference: evidence.Reference{
			Identity: identity, APIVersion: info.APIVersion, ResourceVersion: text(object, false, "metadata", "resourceVersion"),
			CollectedAt: evidence.Timestamp{State: evidence.ValueReported, Value: &collected},
		},
		CreatedAt:  timestamp(object, "metadata", "creationTimestamp"),
		Generation: integer(object, "metadata", "generation"), ObservedGeneration: integer(object, "status", "observedGeneration"),
		DesiredVersion: version(object, "spec", "version"), ObservedVersion: version(object, "status", "currentVersion"),
		Phase:           text(object, true, "status", "phase"),
		DesiredReplicas: count(object, "spec", "replicas"), ReadyReplicas: count(object, "status", "readyReplicas"),
		Paused: boolean(object, "spec", "paused"), Conditions: projectConditions(object, options.Limits.MaxConditions),
	}
	if options.Contract == V1Alpha1Snapshot {
		inferOmissions(object, &result)
	}
	if result.Validate() != nil {
		return evidence.Cluster{}, &ProjectionError{Code: InvalidInput}
	}
	return result, nil
}

func inferOmissions(object map[string]any, result *evidence.Cluster) {
	if result.ReadyReplicas.State == evidence.ValueUnknown && result.DesiredReplicas.State == evidence.ValueReported &&
		result.Acknowledgement() == evidence.GenerationCurrent {
		zero := int64(0)
		result.ReadyReplicas = evidence.Integer{State: evidence.ValueImplied, Value: &zero}
	}
	if result.Paused.State == evidence.ValueUnknown {
		if spec, state := field(object, "spec"); state == evidence.ValueReported {
			if _, ok := spec.(map[string]any); ok {
				no := false
				result.Paused = evidence.Boolean{State: evidence.ValueImplied, Value: &no}
			}
		}
	}
}

func projectConditions(object map[string]any, limit int) evidence.Conditions {
	value, state := field(object, "status", "conditions")
	if state != evidence.ValueReported {
		return evidence.Conditions{State: state}
	}
	items, ok := value.([]any)
	if !ok || len(items) > limit {
		return evidence.Conditions{State: evidence.ValueInvalid}
	}
	result := evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{}}
	seen := map[string]bool{}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return evidence.Conditions{State: evidence.ValueInvalid}
		}
		name, ok := fields["type"].(string)
		status, _ := fields["status"].(string)
		if !ok || !identifier.MatchString(name) || seen[name] || !slices.Contains([]string{"True", "False", "Unknown"}, status) {
			return evidence.Conditions{State: evidence.ValueInvalid}
		}
		seen[name] = true
		observed := integer(fields, "observedGeneration")
		if observed.State == evidence.ValueInvalid {
			return evidence.Conditions{State: evidence.ValueInvalid}
		}
		result.Values = append(result.Values, evidence.Condition{
			Type: name, Status: evidence.ConditionStatus(status), Reason: text(fields, true, "reason"),
			ObservedGeneration: observed, LastTransitionAt: timestamp(fields, "lastTransitionTime"),
		})
	}
	slices.SortFunc(result.Values, func(a, b evidence.Condition) int { return strings.Compare(a.Type, b.Type) })
	return result
}
