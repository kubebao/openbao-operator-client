package adapter

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// Page contains projected observations and transient pagination metadata.
// Continue is opaque control data and is excluded from serialization.
type Page[T any] struct {
	Observations    []T    `json:"observations"`
	ResourceVersion string `json:"resourceVersion"`
	Continue        string `json:"-"`
}

func ProjectClusterList(raw []byte, options ProjectionOptions) (Page[evidence.Cluster], error) {
	return projectList(raw, options, "OpenBaoCluster", ProjectCluster)
}

func ProjectRestoreList(raw []byte, options ProjectionOptions) (Page[evidence.Restore], error) {
	return projectList(raw, options, "OpenBaoRestore", ProjectRestore)
}

func ProjectStatefulSetList(raw []byte, options ProjectionOptions) (Page[evidence.StatefulSet], error) {
	return projectList(raw, options, "StatefulSet", ProjectStatefulSet)
}

func ProjectPodList(raw []byte, options ProjectionOptions) (Page[evidence.Pod], error) {
	return projectList(raw, options, "Pod", ProjectPod)
}

func projectList[T any](raw []byte, options ProjectionOptions, kind string,
	project func([]byte, ProjectionOptions) (T, error),
) (Page[T], error) {
	info, err := Describe(options.Contract)
	if err != nil {
		return Page[T]{}, err
	}
	if !slices.Contains(info.Kinds, kind) {
		return Page[T]{}, &ProjectionError{Code: UnsupportedContract}
	}
	origin := evidence.Text{State: evidence.ValueReported, Value: &options.Origin}
	collected := evidence.Timestamp{State: evidence.ValueReported, Value: &options.CollectedAt}
	if origin.Validate() != nil || len(options.Origin) > 253 || collected.Validate() != nil {
		return Page[T]{}, &ProjectionError{Code: InvalidInput}
	}
	root, err := decodeObject(raw, options.Limits)
	if err != nil {
		return Page[T]{}, err
	}
	if required(root, "apiVersion") != info.APIVersion || required(root, "kind") != kind+"List" {
		return Page[T]{}, &ProjectionError{Code: ResourceMismatch}
	}
	version := text(root, false, "metadata", "resourceVersion")
	if version.State != evidence.ValueReported {
		return Page[T]{}, &ProjectionError{Code: InvalidInput}
	}
	next, err := continuation(root)
	if err != nil {
		return Page[T]{}, err
	}
	items, ok := root["items"].([]any)
	if !ok {
		return Page[T]{}, &ProjectionError{Code: InvalidInput}
	}
	if len(items) > options.Limits.MaxEntries {
		return Page[T]{}, &ProjectionError{Code: ProjectionLimit}
	}
	result := Page[T]{Observations: []T{}, ResourceVersion: *version.Value, Continue: next}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return Page[T]{}, &ProjectionError{Code: InvalidInput}
		}
		// Typed list items can omit TypeMeta. Only absence inherits the envelope;
		// present null, empty, or conflicting declarations fail resource projection.
		if _, exists := object["apiVersion"]; !exists {
			object["apiVersion"] = info.APIVersion
		}
		if _, exists := object["kind"]; !exists {
			object["kind"] = kind
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			return Page[T]{}, &ProjectionError{Code: InvalidInput}
		}
		value, err := project(encoded, options)
		if err != nil {
			return Page[T]{}, err
		}
		result.Observations = append(result.Observations, value)
	}
	return result, nil
}

func continuation(root map[string]any) (string, error) {
	value, state := field(root, "metadata", "continue")
	if state == evidence.ValueUnknown {
		return "", nil
	}
	next, ok := value.(string)
	if !ok || state != evidence.ValueReported || len(next) > 4096 ||
		strings.ContainsFunc(next, unicode.IsControl) || strings.ContainsRune(next, unicode.ReplacementChar) {
		return "", &ProjectionError{Code: InvalidInput}
	}
	return next, nil
}
