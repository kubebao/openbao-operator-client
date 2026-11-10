package evidence

// CollectionOutcome records one scope's outcome without embedding raw client errors.
type CollectionOutcome string

const (
	CollectionNotRequested   CollectionOutcome = "not_requested"
	CollectionNotCollected   CollectionOutcome = "not_collected"
	CollectionComplete       CollectionOutcome = "complete"
	CollectionPartial        CollectionOutcome = "partial"
	CollectionDenied         CollectionOutcome = "denied"
	CollectionUnsupported    CollectionOutcome = "unsupported"
	CollectionTimeout        CollectionOutcome = "timeout"
	CollectionCancelled      CollectionOutcome = "cancelled"
	CollectionPageLimit      CollectionOutcome = "page_limit"
	CollectionSizeLimit      CollectionOutcome = "size_limit"
	CollectionPaginationLoop CollectionOutcome = "pagination_loop"
	CollectionDeleted        CollectionOutcome = "deleted"
	CollectionReplaced       CollectionOutcome = "replaced"
	CollectionInvalid        CollectionOutcome = "invalid"
	CollectionFailed         CollectionOutcome = "failed"
)

func (o CollectionOutcome) Valid() bool {
	switch o {
	case CollectionNotRequested, CollectionNotCollected, CollectionComplete, CollectionPartial, CollectionDenied,
		CollectionUnsupported, CollectionTimeout, CollectionCancelled, CollectionPageLimit, CollectionSizeLimit,
		CollectionPaginationLoop, CollectionDeleted, CollectionReplaced, CollectionInvalid, CollectionFailed:
		return true
	default:
		return false
	}
}

type ReadAction string

const (
	ReadDiscovery ReadAction = "discovery"
	ReadGet       ReadAction = "get"
	ReadList      ReadAction = "list"
)

// Scope declares the origin and exact requested resource boundary. An empty namespace
// means an all-namespace request; callers must authorize that scope separately.
type Scope struct {
	Origin    string     `json:"origin"`
	Group     string     `json:"group"`
	Version   string     `json:"version"`
	Resource  string     `json:"resource"`
	Namespace string     `json:"namespace"`
	Name      string     `json:"name,omitempty"`
	Selector  string     `json:"selector,omitempty"`
	Action    ReadAction `json:"action"`
}

func (s Scope) Validate() error {
	for _, value := range []string{s.Origin, s.Version, s.Resource} {
		if !validText(value, 253, false) {
			return ErrInvalidEvidence
		}
	}
	for _, value := range []string{s.Group, s.Namespace, s.Name} {
		if !validText(value, 253, true) {
			return ErrInvalidEvidence
		}
	}
	if !validText(s.Selector, 4096, true) {
		return ErrInvalidEvidence
	}
	switch s.Action {
	case ReadGet:
		if s.Name == "" || s.Selector != "" {
			return ErrInvalidEvidence
		}
	case ReadList, ReadDiscovery:
		if s.Name != "" {
			return ErrInvalidEvidence
		}
	default:
		return ErrInvalidEvidence
	}
	return nil
}

// Collection records an attempted scope without implying that an empty result proves absence.
// Items counts candidates before association filtering, not just retained observations.
type Collection struct {
	Scope    Scope             `json:"scope"`
	Interval Interval          `json:"interval"`
	Outcome  CollectionOutcome `json:"outcome"`
	Items    int               `json:"items"`
}

func (c Collection) Validate() error {
	if c.Scope.Validate() != nil || !c.Outcome.Valid() || c.Items < 0 {
		return ErrInvalidEvidence
	}
	if c.Outcome == CollectionNotRequested || c.Outcome == CollectionNotCollected {
		if c.Items != 0 || !c.Interval.StartedAt.IsZero() || !c.Interval.CompletedAt.IsZero() {
			return ErrInvalidEvidence
		}
		return nil
	}
	return c.Interval.Validate()
}

// AbsenceConfirmed applies only to a valid, complete list in its declared scope.
// A named GET, discovery, denied read, or partial list cannot establish scoped absence.
func (c Collection) AbsenceConfirmed() bool {
	return c.Validate() == nil && c.Scope.Action == ReadList && c.Outcome == CollectionComplete && c.Items == 0
}
