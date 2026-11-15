package evidence

import "strings"

// Identity identifies a namespaced resource within a caller-defined Kubernetes origin.
// Version, resource version, generation, and observation time are not stable identity.
// An empty group identifies a core Kubernetes resource.
type Identity struct {
	Origin    string `json:"origin"`
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

// Validate checks that every identity field except the group is approved text.
func (id Identity) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"origin", id.Origin},
		{"kind", id.Kind},
		{"namespace", id.Namespace},
		{"name", id.Name},
		{"uid", id.UID},
	} {
		if !ValidText(field.value, MaxNameLength) {
			return At(field.name, ErrInvalidEvidence)
		}
	}
	if !validOptionalText(id.Group, MaxNameLength) {
		return At("group", ErrInvalidEvidence)
	}
	return nil
}

// Reference identifies a resource read and an optional approved field within it.
// CollectedAt records the read time, not the producer's observation or transition time.
type Reference struct {
	Identity        Identity  `json:"identity"`
	APIVersion      string    `json:"apiVersion"`
	ResourceVersion Text      `json:"resourceVersion"`
	CollectedAt     Timestamp `json:"collectedAt"`
	Field           string    `json:"field,omitempty"`
}

// Validate checks the identity, a reported collection time, and an API version
// whose group matches the identity.
func (r Reference) Validate() error {
	if err := checkFields(
		fieldCheck{"identity", r.Identity.Validate()},
		fieldCheck{"resourceVersion", r.ResourceVersion.Validate()},
		fieldCheck{"collectedAt", r.CollectedAt.Validate()},
	); err != nil {
		return err
	}
	if !ValidText(r.APIVersion, MaxNameLength) {
		return At("apiVersion", ErrInvalidEvidence)
	}
	if r.CollectedAt.State != ValueReported {
		return At("collectedAt", ErrInvalidEvidence)
	}
	if !validOptionalText(r.Field, maxFieldLength) {
		return At("field", ErrInvalidEvidence)
	}
	group, version, qualified := strings.Cut(r.APIVersion, "/")
	if qualified {
		if group != r.Identity.Group || version == "" || strings.Contains(version, "/") {
			return At("apiVersion", ErrInvalidEvidence)
		}
	} else if r.Identity.Group != "" {
		return At("apiVersion", ErrInvalidEvidence)
	}
	return nil
}
