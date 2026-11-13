package operations_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

func jsonPlan() operations.Plan {
	at := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	version, generation := "10", int64(4)
	return operations.Plan{
		Contract: adapter.OpenBaoV1Alpha1, Control: operations.ControlPause, Enabled: true,
		MaintenanceState: evidence.ObjectUnknown, Generation: evidence.Integer{State: evidence.ValueReported, Value: &generation},
		Target: evidence.Reference{
			Identity: evidence.Identity{
				Origin:    "test",
				Group:     "openbao.org",
				Kind:      "OpenBaoCluster",
				Namespace: "team",
				Name:      "bao",
				UID:       "uid-1",
			},
			APIVersion:      "openbao.org/v1alpha1",
			ResourceVersion: evidence.Text{State: evidence.ValueReported, Value: &version},
			CollectedAt:     evidence.Timestamp{State: evidence.ValueReported, Value: &at},
		},
	}
}

func TestPlanJSONRequiresUnambiguousIntent(t *testing.T) {
	t.Parallel()
	plan := jsonPlan()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	for _, tc := range []struct{ name, raw string }{
		{"missing enabled", strings.Replace(raw, `"enabled":true,`, "", 1)},
		{"null enabled", strings.Replace(raw, `"enabled":true`, `"enabled":null`, 1)},
		{"wrong type", strings.Replace(raw, `"enabled":true`, `"enabled":"PRIVATE_CANARY"`, 1)},
		{"duplicate intent", strings.Replace(raw, `"enabled":true`, `"enabled":true,"enabled":false`, 1)},
		{"escaped duplicate", strings.Replace(raw, `"enabled":true`, `"enabled":true,"\u0065nabled":false`, 1)},
		{"cased intent", strings.Replace(raw, `"enabled":true`, `"enabled":true,"Enabled":false`, 1)},
		{"cased identity", strings.Replace(raw, `"uid":"uid-1"`, `"uid":"uid-1","UID":"uid-2"`, 1)},
		{"cased value", strings.Replace(raw, `"value":"10"`, `"value":"10","Value":"11"`, 1)},
		{"noncanonical intent", strings.Replace(raw, `"enabled":true`, `"Enabled":false`, 1)},
		{"nested duplicate", strings.Replace(raw, `"uid":"uid-1"`, `"uid":"PRIVATE_CANARY","uid":"uid-1"`, 1)},
		{"unknown field", strings.Replace(raw, `"enabled":true`, `"enabled":true,"token":"PRIVATE_CANARY"`, 1)},
		{"nested unknown field", strings.Replace(raw, `"uid":"uid-1"`, `"uid":"uid-1","token":"PRIVATE_CANARY"`, 1)},
		{"trailing document", raw + `{}`},
		{"null plan", `null`},
		{"oversized", strings.Replace(raw, `"origin":"test"`, `"origin":"`+strings.Repeat("x", 16*1024)+`"`, 1)},
		{"invalid utf8", strings.Replace(raw, "test", string([]byte{0xff}), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := jsonPlan()
			if err := json.Unmarshal([]byte(tc.raw), &got); err == nil {
				t.Fatal("ambiguous serialized intent was accepted")
			} else if strings.Contains(err.Error(), "CANARY") {
				t.Fatal("plan error exposed source text")
			}
			if !reflect.DeepEqual(got, plan) {
				t.Fatal("invalid input changed the destination plan")
			}
		})
	}
}

func TestPlanJSONPreservesExplicitDisable(t *testing.T) {
	t.Parallel()
	for _, control := range []operations.Control{operations.ControlPause, operations.ControlMaintenance} {
		plan := jsonPlan()
		plan.Control, plan.Enabled = control, false
		if control == operations.ControlMaintenance {
			plan.MaintenanceState = evidence.ObjectReported
		}
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		var replay operations.Plan
		if err := json.Unmarshal(raw, &replay); err != nil || !reflect.DeepEqual(replay, plan) {
			t.Fatalf("explicit false did not survive replay: %v", err)
		}
	}
	var pointer *operations.Plan
	if err := pointer.UnmarshalJSON([]byte(`{}`)); !errors.Is(err, operations.ErrInvalidPlan) {
		t.Fatal("nil destination lost error classification")
	}
}
