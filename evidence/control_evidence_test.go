package evidence_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func controlClusterFixture(t *testing.T) evidence.Cluster {
	t.Helper()
	raw, err := os.ReadFile("../diagnose/testdata/cluster.json")
	if err != nil {
		t.Fatal(err)
	}
	var cluster evidence.Cluster
	if err := json.Unmarshal(raw, &cluster); err != nil {
		t.Fatal(err)
	}
	cluster.DeletionTimestamp = evidence.Timestamp{State: evidence.ValueUnknown}
	if err := cluster.Validate(); err != nil {
		t.Fatal(err)
	}
	return cluster
}

func TestClusterDeletionTimestampValidation(t *testing.T) {
	t.Parallel()
	instant := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	zero := time.Time{}
	for _, tc := range []struct {
		name  string
		value evidence.Timestamp
		valid bool
	}{
		{"unknown", evidence.Timestamp{State: evidence.ValueUnknown}, true},
		{"invalid source", evidence.Timestamp{State: evidence.ValueInvalid}, true},
		{"reported", evidence.Timestamp{State: evidence.ValueReported, Value: &instant}, true},
		{"zero state", evidence.Timestamp{}, false},
		{"implied", evidence.Timestamp{State: evidence.ValueImplied, Value: &zero}, false},
		{"reported without value", evidence.Timestamp{State: evidence.ValueReported}, false},
		{"reported zero", evidence.Timestamp{State: evidence.ValueReported, Value: &zero}, false},
		{"unknown with value", evidence.Timestamp{State: evidence.ValueUnknown, Value: &instant}, false},
		{"invalid with value", evidence.Timestamp{State: evidence.ValueInvalid, Value: &instant}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cluster := controlClusterFixture(t)
			cluster.DeletionTimestamp = tc.value
			err := cluster.Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, valid = %t", err, tc.valid)
			}
			if !tc.valid {
				var field *evidence.ValidationError
				if !errors.Is(err, evidence.ErrInvalidEvidence) || !errors.As(err, &field) || field.Path != "deletionTimestamp" {
					t.Fatalf("invalid deletion evidence has no field classification: %v", err)
				}
			}
		})
	}
}

func TestClusterControlEvidenceRetainsRequiredValueStates(t *testing.T) {
	t.Parallel()
	cluster := controlClusterFixture(t)
	cluster.Paused = evidence.Boolean{}
	if err := cluster.Validate(); !errors.Is(err, evidence.ErrInvalidEvidence) {
		t.Fatal("zero value state accepted")
	}
}
