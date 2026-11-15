package evidence

import "time"

// Interval records one bounded collection attempt. It is not an atomic cluster snapshot.
type Interval struct {
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// Validate checks that both instants are valid and that the interval does not end before it starts.
func (i Interval) Validate() error {
	if !validInstant(i.StartedAt) {
		return At("startedAt", ErrInvalidEvidence)
	}
	if !validInstant(i.CompletedAt) || i.CompletedAt.Before(i.StartedAt) {
		return At("completedAt", ErrInvalidEvidence)
	}
	return nil
}

// Contains reports whether at falls within a valid interval, including its endpoints.
func (i Interval) Contains(at time.Time) bool {
	return i.Validate() == nil && !at.Before(i.StartedAt) && !at.After(i.CompletedAt)
}

// Freshness classifies a producer timestamp against a reference time and age limit.
type Freshness string

const (
	// FreshnessUnknown means the producer reported no timestamp.
	FreshnessUnknown Freshness = "unknown"
	// FreshnessFresh means the producer timestamp is within the age limit.
	FreshnessFresh Freshness = "fresh"
	// FreshnessStale means the producer timestamp exceeds the age limit.
	FreshnessStale Freshness = "stale"
	// FreshnessInvalid means an input is invalid.
	FreshnessInvalid Freshness = "invalid"
	// FreshnessInconsistent means the timestamps contradict each other.
	FreshnessInconsistent Freshness = "inconsistent"
)

// AssessFreshness compares a producer timestamp with a supplied reference and age limit.
// Transition timestamps and generation acknowledgement do not establish freshness.
// No clock-skew tolerance is applied.
func AssessFreshness(
	interval Interval,
	collectedAt time.Time,
	producer Timestamp,
	reference time.Time,
	maxAge time.Duration,
) Freshness {
	if !validInstant(interval.StartedAt) || !validInstant(interval.CompletedAt) {
		return FreshnessInvalid
	}
	if !validInstant(collectedAt) || !validInstant(reference) || maxAge <= 0 {
		return FreshnessInvalid
	}
	if producer.Validate() != nil || producer.State == ValueInvalid {
		return FreshnessInvalid
	}
	if !interval.Contains(collectedAt) || reference.Before(interval.CompletedAt) {
		return FreshnessInconsistent
	}
	if producer.State == ValueUnknown {
		return FreshnessUnknown
	}
	if producer.Value.After(collectedAt) {
		return FreshnessInconsistent
	}
	if reference.Sub(*producer.Value) > maxAge {
		return FreshnessStale
	}
	return FreshnessFresh
}
