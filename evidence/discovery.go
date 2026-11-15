package evidence

// ReadVerb is an advertised read verb for a discovered resource.
type ReadVerb string

const (
	// VerbGet is the get verb.
	VerbGet ReadVerb = "get"
	// VerbList is the list verb.
	VerbList ReadVerb = "list"
	// VerbWatch is the watch verb.
	VerbWatch ReadVerb = "watch"
)

// ReadVerbs is a sorted, unique set of advertised read verbs.
type ReadVerbs struct {
	State  ValueState `json:"state"`
	Values []ReadVerb `json:"values"`
}

// Validate checks the set's presence contract and that reported verbs are known, sorted, and unique.
func (v ReadVerbs) Validate() error {
	reported, err := validateSet(v.State, v.Values)
	if !reported {
		return err
	}
	for i, verb := range v.Values {
		switch verb {
		case VerbGet, VerbList, VerbWatch:
		default:
			return At(itemPath("values", i), ErrInvalidEvidence)
		}
		if i > 0 && v.Values[i-1] >= verb {
			return At(itemPath("values", i), ErrInvalidEvidence)
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

// Validate checks a cluster-wide discovery scope, a reported collection time,
// and that an absent resource retains no advertised facts.
func (v Discovery) Validate() error {
	if err := checkFields(
		fieldCheck{"scope", v.Scope.Validate()},
		fieldCheck{"collectedAt", v.CollectedAt.Validate()},
		fieldCheck{"kind", v.Kind.Validate()},
		fieldCheck{"namespaced", v.Namespaced.Validate()},
		fieldCheck{"verbs", v.Verbs.Validate()},
	); err != nil {
		return err
	}
	if v.Scope.Action != ReadDiscovery {
		return At("scope.action", ErrInvalidEvidence)
	}
	if v.Scope.Namespace != "" {
		return At("scope.namespace", ErrInvalidEvidence)
	}
	if v.Scope.Selector != "" {
		return At("scope.selector", ErrInvalidEvidence)
	}
	if v.CollectedAt.State != ValueReported {
		return At("collectedAt", ErrInvalidEvidence)
	}
	if v.Namespaced.State == ValueImplied {
		return At("namespaced", ErrInvalidEvidence)
	}
	switch v.State {
	case ObjectAbsent:
		for _, field := range []struct {
			name  string
			state ValueState
		}{
			{"kind", v.Kind.State},
			{"namespaced", v.Namespaced.State},
			{"verbs", v.Verbs.State},
		} {
			if field.state != ValueUnknown {
				return At(field.name, ErrInvalidEvidence)
			}
		}
		return nil
	case ObjectReported:
		return nil
	default:
		return At("state", ErrInvalidEvidence)
	}
}
