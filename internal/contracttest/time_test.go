package contracttest

import (
	"testing"
	"time"
)

type times struct {
	Started    string `json:"started"`
	Completed  string `json:"completed"`
	Collected  string `json:"collected"`
	Producer   string `json:"producer"`
	Transition string `json:"transition"`
	Reference  string `json:"reference"`
	MaxAge     int    `json:"maxAge"`
}

func freshness(input times) string {
	started, e1 := time.Parse(time.RFC3339Nano, input.Started)
	completed, e2 := time.Parse(time.RFC3339Nano, input.Completed)
	collected, e3 := time.Parse(time.RFC3339Nano, input.Collected)
	reference, e4 := time.Parse(time.RFC3339Nano, input.Reference)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || input.MaxAge <= 0 {
		return "invalid"
	}
	if completed.Before(started) || collected.Before(started) || collected.After(completed) || completed.After(reference) {
		return "inconsistent"
	}
	if input.Producer == "" {
		return "unknown"
	}
	producer, err := time.Parse(time.RFC3339Nano, input.Producer)
	if err != nil {
		return "invalid"
	}
	if producer.After(collected) {
		return "inconsistent"
	}
	if reference.Sub(producer) > time.Duration(input.MaxAge)*time.Second {
		return "stale"
	}
	return "fresh"
}

func TestTimeSemantics(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name  string `json:"name"`
		Times times  `json:"times"`
		Want  string `json:"want"`
	}](t, "time")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			equal(t, freshness(tc.Times), tc.Want)
			tc.Times.Transition = "2026-11-10T12:00:00Z"
			equal(t, freshness(tc.Times), tc.Want)
		})
	}
}
