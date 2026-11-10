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

func (id Identity) Validate() error {
	for _, value := range []string{id.Origin, id.Kind, id.Namespace, id.Name, id.UID} {
		if !validText(value, 253, false) {
			return ErrInvalidEvidence
		}
	}
	if !validText(id.Group, 253, true) {
		return ErrInvalidEvidence
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

func (r Reference) Validate() error {
	if r.Identity.Validate() != nil || !validText(r.APIVersion, 253, false) ||
		r.ResourceVersion.Validate() != nil || r.CollectedAt.Validate() != nil ||
		r.CollectedAt.State != ValueReported || !validText(r.Field, 512, true) {
		return ErrInvalidEvidence
	}
	group, version, qualified := strings.Cut(r.APIVersion, "/")
	if qualified {
		if group != r.Identity.Group || version == "" || strings.Contains(version, "/") {
			return ErrInvalidEvidence
		}
	} else if r.Identity.Group != "" {
		return ErrInvalidEvidence
	}
	return nil
}
