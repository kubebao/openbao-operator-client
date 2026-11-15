package evidence

// CollectionOutcome records one scope's outcome without embedding raw client errors.
type CollectionOutcome string

const (
	// CollectionNotRequested means the caller did not request the scope.
	CollectionNotRequested CollectionOutcome = "not_requested"
	// CollectionNotCollected means the scope was requested but no read was attempted.
	CollectionNotCollected CollectionOutcome = "not_collected"
	// CollectionComplete means every requested page was read and admitted.
	CollectionComplete CollectionOutcome = "complete"
	// CollectionPartial means the read stopped before it completed.
	CollectionPartial CollectionOutcome = "partial"
	// CollectionDenied means the API server rejected the caller's credentials or authorization.
	CollectionDenied CollectionOutcome = "denied"
	// CollectionExpired means a LIST continuation expired.
	CollectionExpired CollectionOutcome = "expired"
	// CollectionRateLimited means the API server asked the caller to retry later.
	CollectionRateLimited CollectionOutcome = "rate_limited"
	// CollectionUnsupported means the API server does not serve the requested resource.
	CollectionUnsupported CollectionOutcome = "unsupported"
	// CollectionTimeout means the read exceeded its deadline.
	CollectionTimeout CollectionOutcome = "timeout"
	// CollectionCancelled means the caller cancelled the read.
	CollectionCancelled CollectionOutcome = "cancelled"
	// CollectionPageLimit means the read reached its page limit.
	CollectionPageLimit CollectionOutcome = "page_limit"
	// CollectionSizeLimit means a response or retained observation exceeded its byte or candidate limit.
	CollectionSizeLimit CollectionOutcome = "size_limit"
	// CollectionPaginationLoop means the API server repeated a continuation token.
	CollectionPaginationLoop CollectionOutcome = "pagination_loop"
	// CollectionDeleted means a named GET found no resource.
	CollectionDeleted CollectionOutcome = "deleted"
	// CollectionReplaced means a named GET found a resource with a different UID.
	CollectionReplaced CollectionOutcome = "replaced"
	// CollectionInvalid means a response could not be projected or violated its scope.
	CollectionInvalid CollectionOutcome = "invalid"
	// CollectionFailed means the read failed for another reason.
	CollectionFailed CollectionOutcome = "failed"
)

// Valid reports whether o is a defined outcome.
func (o CollectionOutcome) Valid() bool {
	switch o {
	case CollectionNotRequested,
		CollectionNotCollected,
		CollectionComplete,
		CollectionPartial,
		CollectionDenied,
		CollectionExpired,
		CollectionRateLimited,
		CollectionUnsupported,
		CollectionTimeout,
		CollectionCancelled,
		CollectionPageLimit,
		CollectionSizeLimit,
		CollectionPaginationLoop,
		CollectionDeleted,
		CollectionReplaced,
		CollectionInvalid,
		CollectionFailed:
		return true
	default:
		return false
	}
}

// ReadAction identifies the kind of read that a scope declares.
type ReadAction string

const (
	// ReadDiscovery reads API discovery for one group and version.
	ReadDiscovery ReadAction = "discovery"
	// ReadGet reads one named resource.
	ReadGet ReadAction = "get"
	// ReadList reads every resource in a namespace or, when authorized, all namespaces.
	ReadList ReadAction = "list"
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

// Validate checks bounded scope fields and the fields that each action permits.
func (s Scope) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"origin", s.Origin},
		{"version", s.Version},
		{"resource", s.Resource},
	} {
		if !ValidText(field.value, MaxNameLength) {
			return At(field.name, ErrInvalidEvidence)
		}
	}
	for _, field := range []struct{ name, value string }{
		{"group", s.Group},
		{"namespace", s.Namespace},
		{"name", s.Name},
	} {
		if !validOptionalText(field.value, MaxNameLength) {
			return At(field.name, ErrInvalidEvidence)
		}
	}
	if !validOptionalText(s.Selector, maxSelectorLength) {
		return At("selector", ErrInvalidEvidence)
	}
	switch s.Action {
	case ReadGet:
		if s.Name == "" {
			return At("name", ErrInvalidEvidence)
		}
		if s.Selector != "" {
			return At("selector", ErrInvalidEvidence)
		}
	case ReadList, ReadDiscovery:
		if s.Name != "" {
			return At("name", ErrInvalidEvidence)
		}
	default:
		return At("action", ErrInvalidEvidence)
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

// Validate checks the scope, outcome, and item count. A read that was not
// attempted has no items and no interval; every other read has a valid interval.
func (c Collection) Validate() error {
	if err := c.Scope.Validate(); err != nil {
		return At("scope", err)
	}
	if !c.Outcome.Valid() {
		return At("outcome", ErrInvalidEvidence)
	}
	if c.Items < 0 {
		return At("items", ErrInvalidEvidence)
	}
	if c.Outcome == CollectionNotRequested || c.Outcome == CollectionNotCollected {
		if c.Items != 0 {
			return At("items", ErrInvalidEvidence)
		}
		if !c.Interval.StartedAt.IsZero() || !c.Interval.CompletedAt.IsZero() {
			return At("interval", ErrInvalidEvidence)
		}
		return nil
	}
	return At("interval", c.Interval.Validate())
}

// AbsenceConfirmed applies only to a valid, complete list in its declared scope.
// A named GET, discovery, denied read, or partial list cannot establish scoped absence.
func (c Collection) AbsenceConfirmed() bool {
	return c.Validate() == nil && c.Scope.Action == ReadList && c.Outcome == CollectionComplete && c.Items == 0
}
