package adapter

import (
	"slices"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// projectResource decodes one resource of a kind that the contract covers and
// returns its validated reference.
func projectResource(raw []byte, options ProjectionOptions, kind string) (map[string]any, evidence.Reference, error) {
	info, err := Describe(options.Contract)
	if err != nil {
		return nil, evidence.Reference{}, err
	}
	if !slices.Contains(info.Kinds, kind) {
		return nil, evidence.Reference{}, &ProjectionError{Code: CodeUnsupportedContract}
	}
	if options.CollectedAt.IsZero() {
		return nil, evidence.Reference{}, &ProjectionError{Code: CodeInvalidInput}
	}
	object, err := decodeObject(raw, options.Limits)
	if err != nil {
		return nil, evidence.Reference{}, err
	}
	if required(object, "apiVersion") != info.APIVersion {
		return nil, evidence.Reference{}, &ProjectionError{Code: CodeResourceMismatch}
	}
	if required(object, "kind") != kind {
		return nil, evidence.Reference{}, &ProjectionError{Code: CodeResourceMismatch}
	}
	collected := options.CollectedAt.UTC()
	ref := evidence.Reference{
		Identity: evidence.Identity{
			Origin:    options.Origin,
			Group:     info.APIGroup,
			Kind:      kind,
			Namespace: required(object, "metadata", "namespace"),
			Name:      required(object, "metadata", "name"),
			UID:       required(object, "metadata", "uid"),
		},
		APIVersion:      info.APIVersion,
		ResourceVersion: text(object, false, "metadata", "resourceVersion"),
		CollectedAt:     evidence.Timestamp{State: evidence.ValueReported, Value: &collected},
	}
	if ref.Validate() != nil {
		return nil, evidence.Reference{}, &ProjectionError{Code: CodeInvalidInput}
	}
	return object, ref, nil
}
