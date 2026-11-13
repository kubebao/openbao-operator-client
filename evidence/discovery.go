package evidence

import "slices"

type ReadVerb string

const (
	VerbGet   ReadVerb = "get"
	VerbList  ReadVerb = "list"
	VerbWatch ReadVerb = "watch"
)

type ReadVerbs struct {
	State  ValueState `json:"state"`
	Values []ReadVerb `json:"values"`
}

func (v ReadVerbs) Validate() error {
	if v.State == ValueUnknown || v.State == ValueInvalid {
		if v.Values == nil {
			return nil
		}
		return ErrInvalidEvidence
	}
	if v.State != ValueReported || v.Values == nil {
		return ErrInvalidEvidence
	}
	for i, verb := range v.Values {
		if !slices.Contains([]ReadVerb{VerbGet, VerbList, VerbWatch}, verb) || i > 0 && v.Values[i-1] >= verb {
			return ErrInvalidEvidence
		}
	}
	return nil
}

// Discovery records advertised facts for one resource in one group/version.
// It establishes neither caller authorization nor operator integration qualification.
type Discovery struct {
	Scope       Scope       `json:"scope"`
	CollectedAt Timestamp   `json:"collectedAt"`
	State       ObjectState `json:"state"`
	Kind        Text        `json:"kind"`
	Namespaced  Boolean     `json:"namespaced"`
	Verbs       ReadVerbs   `json:"verbs"`
}

func (v Discovery) Validate() error {
	if v.Scope.Validate() != nil ||
		v.Scope.Action != ReadDiscovery || v.Scope.Namespace != "" || v.Scope.Selector != "" ||
		v.CollectedAt.Validate() != nil || v.CollectedAt.State != ValueReported || v.Kind.Validate() != nil ||
		v.Namespaced.Validate() != nil || v.Namespaced.State == ValueImplied || v.Verbs.Validate() != nil {
		return ErrInvalidEvidence
	}
	if v.State == ObjectAbsent {
		if v.Kind.State != ValueUnknown || v.Namespaced.State != ValueUnknown || v.Verbs.State != ValueUnknown {
			return ErrInvalidEvidence
		}
		return nil
	}
	if v.State != ObjectReported {
		return ErrInvalidEvidence
	}
	return nil
}
