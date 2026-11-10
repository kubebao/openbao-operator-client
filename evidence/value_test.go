package evidence_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type validatedValue interface{ Validate() error }

func TestNormalizedValueValidation(t *testing.T) {
	t.Parallel()
	zero, one, yes, no, text := int64(0), int64(1), true, false, "value"
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		value validatedValue
		valid bool
	}{
		{"unknown-integer", evidence.Integer{State: evidence.ValueUnknown}, true},
		{"invalid-integer", evidence.Integer{State: evidence.ValueInvalid}, true},
		{"reported-zero", evidence.Integer{State: evidence.ValueReported, Value: &zero}, true},
		{"reported-without-value", evidence.Integer{State: evidence.ValueReported}, false},
		{"unknown-with-value", evidence.Integer{State: evidence.ValueUnknown, Value: &one}, false},
		{"invalid-retained-value", evidence.Integer{State: evidence.ValueInvalid, Value: &one}, false},
		{"implied-zero", evidence.Integer{State: evidence.ValueImplied, Value: &zero}, true},
		{"implied-positive", evidence.Integer{State: evidence.ValueImplied, Value: &one}, false},
		{"unknown-state", evidence.Integer{State: "future"}, false},
		{"reported-false", evidence.Boolean{State: evidence.ValueReported, Value: &no}, true},
		{"implied-false", evidence.Boolean{State: evidence.ValueImplied, Value: &no}, true},
		{"implied-true", evidence.Boolean{State: evidence.ValueImplied, Value: &yes}, false},
		{"text", evidence.Text{State: evidence.ValueReported, Value: &text}, true},
		{"implied-text", evidence.Text{State: evidence.ValueImplied, Value: &text}, false},
		{"timestamp", evidence.Timestamp{State: evidence.ValueReported, Value: &now}, true},
		{"implied-time", evidence.Timestamp{State: evidence.ValueImplied, Value: &now}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.value.Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, valid = %t", err, tc.valid)
			}
			if err != nil && !errors.Is(err, evidence.ErrInvalidEvidence) {
				t.Fatal("validation error lost classification")
			}
		})
	}
}

func TestIdentityAndReferenceValidation(t *testing.T) {
	t.Parallel()
	identity := evidence.Identity{
		Origin:    "origin",
		Group:     "openbao.org",
		Kind:      "OpenBaoCluster",
		Namespace: "team",
		Name:      "bao",
		UID:       "uid-1",
	}
	if identity.Validate() != nil {
		t.Fatal("valid identity rejected")
	}
	for _, value := range []string{"", "uid\n1", "uid\uFFFD", strings.Repeat("a", 254)} {
		changed := identity
		changed.UID = value
		if changed.Validate() == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	ref := evidence.Reference{
		Identity:        identity,
		APIVersion:      "openbao.org/v1alpha1",
		ResourceVersion: evidence.Text{State: evidence.ValueUnknown},
		CollectedAt:     evidence.Timestamp{State: evidence.ValueReported, Value: &now},
	}
	if ref.Validate() != nil {
		t.Fatal("valid reference rejected")
	}
	for _, version := range []string{"v1", "other.org/v1", "openbao.org/", "openbao.org/v1/extra"} {
		changed := ref
		changed.APIVersion = version
		if changed.Validate() == nil {
			t.Fatalf("inconsistent reference accepted: %s", version)
		}
	}
	ref.Identity.Group = ""
	ref.Identity.Kind = "Pod"
	ref.APIVersion = "v1"
	if ref.Validate() != nil {
		t.Fatal("core group reference rejected")
	}
}

func TestConditionsValidationAndReplay(t *testing.T) {
	t.Parallel()
	base := evidence.Condition{
		Type:               "Available",
		Status:             evidence.ConditionTrue,
		Reason:             evidence.Text{State: evidence.ValueUnknown},
		ObservedGeneration: evidence.Integer{State: evidence.ValueUnknown},
		LastTransitionAt:   evidence.Timestamp{State: evidence.ValueUnknown},
	}
	for _, tc := range []struct {
		name   string
		values evidence.Conditions
		valid  bool
	}{
		{"unknown", evidence.Conditions{State: evidence.ValueUnknown}, true},
		{"invalid", evidence.Conditions{State: evidence.ValueInvalid}, true},
		{"empty", evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{}}, true},
		{"null", evidence.Conditions{State: evidence.ValueReported}, false},
		{"one", evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{base}}, true},
		{"duplicate", evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{base, base}}, false},
		{"unreported-entries", evidence.Conditions{State: evidence.ValueUnknown, Values: []evidence.Condition{base}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (tc.values.Validate() == nil) != tc.valid {
				t.Fatal("unexpected condition validation")
			}
			encoded, err := json.Marshal(tc.values)
			if err != nil {
				t.Fatal(err)
			}
			var decoded evidence.Conditions
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if (decoded.Validate() == nil) != tc.valid {
				t.Fatal("serialized conditions changed presence")
			}
		})
	}
	one := int64(1)
	base.ObservedGeneration = evidence.Integer{State: evidence.ValueUnknown, Value: &one}
	if base.Acknowledgement(evidence.Integer{State: evidence.ValueReported, Value: &one}) == evidence.GenerationCurrent {
		t.Fatal("malformed generation became current")
	}
}
