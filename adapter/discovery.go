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
	kind := resourceKind(resource)
	if !slices.Contains(info.Kinds, kind) {
		return "", &ProjectionError{Code: CodeUnsupportedContract}
	}
	return kind, nil
}

// resourceKind returns the kind of a supported plural resource name, or "".
func resourceKind(resource string) string {
	switch resource {
	case "openbaoclusters":
		return "OpenBaoCluster"
	case "openbaorestores":
		return "OpenBaoRestore"
	case "statefulsets":
		return "StatefulSet"
	case "pods":
		return "Pod"
	default:
		return ""
	}
}

// ProjectDiscovery projects the advertised facts for one resource from an
// APIResourceList document. A resource missing from the list is reported absent.
// Group and version overrides must match the discovery endpoint.
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
	if required(root, "apiVersion") != "v1" || required(root, "kind") != "APIResourceList" {
		return evidence.Discovery{}, &ProjectionError{Code: CodeResourceMismatch}
	}
	if required(root, "groupVersion") != info.APIVersion {
		return evidence.Discovery{}, &ProjectionError{Code: CodeResourceMismatch}
	}
	items, ok := root["resources"].([]any)
	if !ok {
		return evidence.Discovery{}, &ProjectionError{Code: CodeInvalidInput}
	}
	if len(items) > options.Limits.MaxEntries {
		return evidence.Discovery{}, &ProjectionError{Code: CodeProjectionLimit}
	}
	collected := options.CollectedAt.UTC()
	result := evidence.Discovery{
		Scope: evidence.Scope{
			Origin:   options.Origin,
			Group:    info.APIGroup,
			Version:  strings.TrimPrefix(info.APIVersion, info.APIGroup+"/"),
			Resource: resource,
			Action:   evidence.ReadDiscovery,
		},
		CollectedAt: evidence.Timestamp{State: evidence.ValueReported, Value: &collected},
		State:       evidence.ObjectAbsent,
		Kind:        evidence.Text{State: evidence.ValueUnknown},
		Namespaced:  evidence.Boolean{State: evidence.ValueUnknown},
		Verbs:       evidence.ReadVerbs{State: evidence.ValueUnknown},
	}
	seen := map[string]bool{}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return evidence.Discovery{}, &ProjectionError{Code: CodeInvalidInput}
		}
		name := required(object, "name")
		if name == "" || seen[name] {
			return evidence.Discovery{}, &ProjectionError{Code: CodeInvalidInput}
		}
		seen[name] = true
		if name != resource {
			continue
		}
		result.State = evidence.ObjectReported
		result.Kind = text(object, true, "kind")
		result.Namespaced = boolean(object, "namespaced")
		result.Verbs = discoveryVerbs(object, options.Limits.MaxEntries)
		// Explicit group/version overrides must remain consistent with this endpoint.
		for _, override := range []struct{ key, expected string }{
			{"group", info.APIGroup},
			{"version", result.Scope.Version},
		} {
			value, exists := object[override.key]
			if exists && value != "" && value != override.expected {
				return evidence.Discovery{}, &ProjectionError{Code: CodeResourceMismatch}
			}
		}
	}
	if result.Validate() != nil {
		return evidence.Discovery{}, &ProjectionError{Code: CodeInvalidInput}
	}
	return result, nil
}

// discoveryVerbs projects the advertised read verbs and drops other verbs.
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
		switch read := evidence.ReadVerb(verb); read {
		case evidence.VerbGet, evidence.VerbList, evidence.VerbWatch:
			result.Values = append(result.Values, read)
		}
	}
	slices.Sort(result.Values)
	return result
}

// Advertises checks a read verb against validated discovery facts. It does not
// establish RBAC access, omission semantics, or integration qualification.
func Advertises(discovery evidence.Discovery, verb evidence.ReadVerb) bool {
	kind := resourceKind(discovery.Scope.Resource)
	if !supportedDiscoveryScope(discovery.Scope, kind) || discovery.Validate() != nil {
		return false
	}
	if discovery.State != evidence.ObjectReported {
		return false
	}
	if discovery.Kind.State != evidence.ValueReported || *discovery.Kind.Value != kind {
		return false
	}
	if discovery.Namespaced.State != evidence.ValueReported || !*discovery.Namespaced.Value {
		return false
	}
	return discovery.Verbs.State == evidence.ValueReported && slices.Contains(discovery.Verbs.Values, verb)
}

// supportedDiscoveryScope reports whether a kind is covered in the scope's group and version.
func supportedDiscoveryScope(scope evidence.Scope, kind string) bool {
	switch kind {
	case "OpenBaoCluster", "OpenBaoRestore":
		return (scope.Group == "openbao.org" || scope.Group == "kubebao.org") && scope.Version == "v1alpha1"
	case "StatefulSet":
		return scope.Group == "apps" && scope.Version == "v1"
	case "Pod":
		return scope.Group == "" && scope.Version == "v1"
	default:
		return false
	}
}
