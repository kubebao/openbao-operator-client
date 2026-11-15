package adapter

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// maxContinueLength bounds an opaque LIST continuation token.
const maxContinueLength = 4096

// Page contains projected observations and transient pagination metadata.
// Continue is opaque control data and is excluded from serialization.
type Page[T any] struct {
	Observations    []T    `json:"observations"`
	ResourceVersion string `json:"resourceVersion"`
	Continue        string `json:"-"`
}

// ProjectClusterList projects one OpenBaoClusterList page with ProjectCluster.
func ProjectClusterList(raw []byte, options ProjectionOptions) (Page[evidence.Cluster], error) {
	return projectList(raw, options, "OpenBaoCluster", ProjectCluster)
}

// ProjectRestoreList projects one OpenBaoRestoreList page with ProjectRestore.
func ProjectRestoreList(raw []byte, options ProjectionOptions) (Page[evidence.Restore], error) {
	return projectList(raw, options, "OpenBaoRestore", ProjectRestore)
}

// ProjectStatefulSetList projects one StatefulSetList page with ProjectStatefulSet.
func ProjectStatefulSetList(raw []byte, options ProjectionOptions) (Page[evidence.StatefulSet], error) {
	return projectList(raw, options, "StatefulSet", ProjectStatefulSet)
}

// ProjectPodList projects one PodList page with ProjectPod.
func ProjectPodList(raw []byte, options ProjectionOptions) (Page[evidence.Pod], error) {
	return projectList(raw, options, "Pod", ProjectPod)
}

// projectList projects every item of one list page. Any invalid item fails the page.
func projectList[T any](
	raw []byte,
	options ProjectionOptions,
	kind string,
	project func([]byte, ProjectionOptions) (T, error),
) (Page[T], error) {
	info, err := Describe(options.Contract)
	if err != nil {
		return Page[T]{}, err
	}
	if !slices.Contains(info.Kinds, kind) {
		return Page[T]{}, &ProjectionError{Code: CodeUnsupportedContract}
	}
	if !evidence.ValidText(options.Origin, evidence.MaxNameLength) {
		return Page[T]{}, &ProjectionError{Code: CodeInvalidInput}
	}
	collected := evidence.Timestamp{State: evidence.ValueReported, Value: &options.CollectedAt}
	if collected.Validate() != nil {
		return Page[T]{}, &ProjectionError{Code: CodeInvalidInput}
	}
	root, err := decodeObject(raw, options.Limits)
	if err != nil {
		return Page[T]{}, err
	}
	if required(root, "apiVersion") != info.APIVersion {
		return Page[T]{}, &ProjectionError{Code: CodeResourceMismatch}
	}
	if required(root, "kind") != kind+"List" {
		return Page[T]{}, &ProjectionError{Code: CodeResourceMismatch}
	}
	version := text(root, false, "metadata", "resourceVersion")
	if version.State != evidence.ValueReported {
		return Page[T]{}, &ProjectionError{Code: CodeInvalidInput}
	}
	next, err := continuation(root)
	if err != nil {
		return Page[T]{}, err
	}
	items, ok := root["items"].([]any)
	if !ok {
		return Page[T]{}, &ProjectionError{Code: CodeInvalidInput}
	}
	if len(items) > options.Limits.MaxEntries {
		return Page[T]{}, &ProjectionError{Code: CodeProjectionLimit}
	}
	result := Page[T]{
		Observations:    []T{},
		ResourceVersion: *version.Value,
		Continue:        next,
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return Page[T]{}, &ProjectionError{Code: CodeInvalidInput}
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
			return Page[T]{}, &ProjectionError{Code: CodeInvalidInput}
		}
		value, err := project(encoded, options)
		if err != nil {
			return Page[T]{}, err
		}
		result.Observations = append(result.Observations, value)
	}
	return result, nil
}

// continuation returns the opaque continue token. An omitted token is empty.
func continuation(root map[string]any) (string, error) {
	value, state := field(root, "metadata", "continue")
	if state == evidence.ValueUnknown {
		return "", nil
	}
	next, ok := value.(string)
	if !ok || state != evidence.ValueReported || len(next) > maxContinueLength {
		return "", &ProjectionError{Code: CodeInvalidInput}
	}
	if strings.ContainsFunc(next, unicode.IsControl) || strings.ContainsRune(next, unicode.ReplacementChar) {
		return "", &ProjectionError{Code: CodeInvalidInput}
	}
	return next, nil
}
