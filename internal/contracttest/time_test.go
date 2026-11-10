package contracttest

import (
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
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
	producer := evidence.Timestamp{State: evidence.ValueUnknown}
	if input.Producer != "" {
		parsed, err := time.Parse(time.RFC3339Nano, input.Producer)
		if err != nil {
			producer.State = evidence.ValueInvalid
		} else {
			producer = evidence.Timestamp{State: evidence.ValueReported, Value: &parsed}
		}
	}
	return string(
		evidence.AssessFreshness(
			evidence.Interval{StartedAt: started, CompletedAt: completed},
			collected,
			producer,
			reference,
			time.Duration(input.MaxAge)*time.Second,
		),
	)
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
