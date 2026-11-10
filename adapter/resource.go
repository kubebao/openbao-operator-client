package adapter

import "github.com/kubebao/openbao-operator-client/evidence"

func projectResource(raw []byte, options ProjectionOptions, kind string) (map[string]any, evidence.Reference, error) {
	info, err := Describe(options.Contract)
	if err != nil {
		return nil, evidence.Reference{}, err
	}
	if options.CollectedAt.IsZero() {
		return nil, evidence.Reference{}, &ProjectionError{Code: InvalidInput}
	}
	object, err := decodeObject(raw, options.Limits)
	if err != nil {
		return nil, evidence.Reference{}, err
	}
	if required(object, "apiVersion") != info.APIVersion || required(object, "kind") != kind {
		return nil, evidence.Reference{}, &ProjectionError{Code: ResourceMismatch}
	}
	collected := options.CollectedAt.UTC()
	ref := evidence.Reference{
		Identity: evidence.Identity{
			Origin: options.Origin,
			Group:  info.APIGroup,
			Kind:   kind,
			Namespace: required(
				object,
				"metadata",
				"namespace",
			),
			Name: required(object, "metadata", "name"),
			UID:  required(object, "metadata", "uid"),
		},
		APIVersion: info.APIVersion, ResourceVersion: text(object, false, "metadata", "resourceVersion"),
		CollectedAt: evidence.Timestamp{State: evidence.ValueReported, Value: &collected},
	}
	if ref.Validate() != nil {
		return nil, evidence.Reference{}, &ProjectionError{Code: InvalidInput}
	}
	return object, ref, nil
}
