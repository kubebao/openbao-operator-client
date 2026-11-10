package evidence

import "time"

// Interval records one bounded collection attempt. It is not an atomic cluster snapshot.
type Interval struct {
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

func (i Interval) Validate() error {
	if !validInstant(i.StartedAt) || !validInstant(i.CompletedAt) || i.CompletedAt.Before(i.StartedAt) {
		return ErrInvalidEvidence
	}
	return nil
}

func (i Interval) Contains(at time.Time) bool {
	return i.Validate() == nil && !at.Before(i.StartedAt) && !at.After(i.CompletedAt)
}

type Freshness string

const (
	FreshnessUnknown      Freshness = "unknown"
	FreshnessFresh        Freshness = "fresh"
	FreshnessStale        Freshness = "stale"
	FreshnessInvalid      Freshness = "invalid"
	FreshnessInconsistent Freshness = "inconsistent"
)

// AssessFreshness compares a producer timestamp with a supplied reference and age limit.
// Transition timestamps and generation acknowledgement do not establish freshness.
// No clock-skew tolerance is applied.
func AssessFreshness(interval Interval, collectedAt time.Time, producer Timestamp, reference time.Time, maxAge time.Duration) Freshness {
	if !validInstant(interval.StartedAt) || !validInstant(interval.CompletedAt) || !validInstant(collectedAt) || !validInstant(reference) ||
		maxAge <= 0 || producer.Validate() != nil || producer.State == ValueInvalid {
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
