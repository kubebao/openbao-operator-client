package adapter

import (
	"slices"
	"strings"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// ResourceKind restricts discovery to resources covered by the selected contract.
func ResourceKind(contract Contract, resource string) (string, error) {
	info, err := Describe(contract)
	if err != nil {
		return "", err
	}
	kind := map[string]string{
		"openbaoclusters": "OpenBaoCluster", "openbaorestores": "OpenBaoRestore", "statefulsets": "StatefulSet", "pods": "Pod",
	}[resource]
	if !slices.Contains(info.Kinds, kind) {
		return "", &ProjectionError{Code: UnsupportedContract}
	}
	return kind, nil
}

func ProjectDiscovery(raw []byte, resource string, options ProjectionOptions) (evidence.Discovery, error) {
	if _, err := ResourceKind(options.Contract, resource); err != nil {
		return evidence.Discovery{}, err
	}
	info, err := Describe(options.Contract)
	if err != nil {
		return evidence.Discovery{}, err
	}
	root, err := decodeObject(raw, options.Limits)
	if err != nil {
		return evidence.Discovery{}, err
	}
	if required(root, "apiVersion") != "v1" || required(root, "kind") != "APIResourceList" ||
		required(root, "groupVersion") != info.APIVersion {
		return evidence.Discovery{}, &ProjectionError{Code: ResourceMismatch}
	}
	items, ok := root["resources"].([]any)
	if !ok {
		return evidence.Discovery{}, &ProjectionError{Code: InvalidInput}
	}
	if len(items) > options.Limits.MaxEntries {
		return evidence.Discovery{}, &ProjectionError{Code: ProjectionLimit}
	}
	collected := options.CollectedAt.UTC()
	result := evidence.Discovery{
		SchemaVersion: evidence.SchemaVersion, AdapterContract: string(options.Contract),
		Scope: evidence.Scope{
			Origin: options.Origin, Group: info.APIGroup,
			Version: strings.TrimPrefix(info.APIVersion, info.APIGroup+"/"), Resource: resource, Action: evidence.ReadDiscovery,
		},
		CollectedAt: evidence.Timestamp{State: evidence.ValueReported, Value: &collected}, State: evidence.ObjectAbsent,
		Kind: evidence.Text{State: evidence.ValueUnknown}, Namespaced: evidence.Boolean{State: evidence.ValueUnknown},
		Verbs: evidence.ReadVerbs{State: evidence.ValueUnknown},
	}
	seen := map[string]bool{}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return evidence.Discovery{}, &ProjectionError{Code: InvalidInput}
		}
		name := required(object, "name")
		if name == "" || seen[name] {
			return evidence.Discovery{}, &ProjectionError{Code: InvalidInput}
		}
		seen[name] = true
		if name != resource {
			continue
		}
		result.State, result.Kind, result.Namespaced = evidence.ObjectReported, text(object, true, "kind"), boolean(object, "namespaced")
		result.Verbs = discoveryVerbs(object, options.Limits.MaxEntries)
		// Explicit group/version overrides must remain consistent with this endpoint.
		for key, expected := range map[string]string{"group": info.APIGroup, "version": result.Scope.Version} {
			value, exists := object[key]
			if exists && value != "" && value != expected {
				return evidence.Discovery{}, &ProjectionError{Code: ResourceMismatch}
			}
		}
	}
	if result.Validate() != nil {
		return evidence.Discovery{}, &ProjectionError{Code: InvalidInput}
	}
	return result, nil
}

func discoveryVerbs(raw map[string]any, limit int) evidence.ReadVerbs {
	value, state := field(raw, "verbs")
	if state != evidence.ValueReported {
		return evidence.ReadVerbs{State: state}
	}
	items, ok := value.([]any)
	if !ok || len(items) > limit {
		return evidence.ReadVerbs{State: evidence.ValueInvalid}
	}
	result := evidence.ReadVerbs{State: evidence.ValueReported, Values: []evidence.ReadVerb{}}
	seen := map[string]bool{}
	for _, item := range items {
		verb, ok := item.(string)
		if !ok || !identifier.MatchString(verb) || seen[verb] {
			return evidence.ReadVerbs{State: evidence.ValueInvalid}
		}
		seen[verb] = true
		if slices.Contains([]string{"get", "list", "watch"}, verb) {
			result.Values = append(result.Values, evidence.ReadVerb(verb))
		}
	}
	slices.Sort(result.Values)
	return result
}

// Advertises checks a read verb against validated discovery facts. It does not
// establish RBAC access, omission semantics, or integration qualification.
func Advertises(discovery evidence.Discovery, verb evidence.ReadVerb) bool {
	kind, err := ResourceKind(Contract(discovery.AdapterContract), discovery.Scope.Resource)
	info, describeErr := Describe(Contract(discovery.AdapterContract))
	return err == nil && describeErr == nil && discovery.Validate() == nil &&
		discovery.Scope.Group == info.APIGroup && discovery.Scope.Version == strings.TrimPrefix(info.APIVersion, info.APIGroup+"/") &&
		discovery.State == evidence.ObjectReported &&
		discovery.Kind.State == evidence.ValueReported && *discovery.Kind.Value == kind &&
		discovery.Namespaced.State == evidence.ValueReported && *discovery.Namespaced.Value &&
		discovery.Verbs.State == evidence.ValueReported && slices.Contains(discovery.Verbs.Values, verb)
}
