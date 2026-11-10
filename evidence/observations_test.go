package evidence_test

import (
	"testing"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestObjectStatesRetainNoUnobservedChildFacts(t *testing.T) {
	t.Parallel()
	reason := "Failed"
	states := []evidence.ObjectState{evidence.ObjectUnknown, evidence.ObjectAbsent, evidence.ObjectNull, evidence.ObjectInvalid}
	for _, state := range states {
		value := evidence.ControllerError{
			State:  state,
			Reason: evidence.Text{State: evidence.ValueUnknown},
			At:     evidence.Timestamp{State: evidence.ValueUnknown},
		}
		if value.Validate() != nil {
			t.Fatalf("object state %s rejected", state)
		}
		value.Reason = evidence.Text{State: evidence.ValueReported, Value: &reason}
		if value.Validate() == nil {
			t.Fatalf("unobserved object %s retained a child fact", state)
		}
	}
}

func TestOrdinalSetsValidateCanonicalOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value evidence.Ordinals
		valid bool
	}{
		{"unknown", evidence.Ordinals{State: evidence.ValueUnknown}, true},
		{"empty", evidence.Ordinals{State: evidence.ValueReported, Values: []int64{}}, true},
		{"ordered", evidence.Ordinals{State: evidence.ValueReported, Values: []int64{0, 2}}, true},
		{"reordered", evidence.Ordinals{State: evidence.ValueReported, Values: []int64{2, 0}}, false},
		{"duplicates", evidence.Ordinals{State: evidence.ValueReported, Values: []int64{2, 2}}, false},
		{"negative", evidence.Ordinals{State: evidence.ValueReported, Values: []int64{-1}}, false},
		{"overflow", evidence.Ordinals{State: evidence.ValueReported, Values: []int64{2147483648}}, false},
		{"null", evidence.Ordinals{State: evidence.ValueReported}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (tc.value.Validate() == nil) != tc.valid {
				t.Fatal("ordinal evidence changed meaning")
			}
		})
	}
}
