package contracttest

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// Value and condition fixtures call production projection. Remaining reference models are test-only.
//
//go:embed testdata/*.json
var fixtureFiles embed.FS

type number struct {
	State string `json:"state"`
	Value *int64 `json:"value,omitempty"`
}

func fixtures[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := fixtureFiles.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var result T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("fixture has trailing content: %v", err)
	}
	rows := reflect.ValueOf(result)
	if rows.Kind() != reflect.Slice || rows.Len() == 0 {
		t.Fatal("fixture must contain named scenarios")
	}
	seen := map[string]bool{}
	for i := 0; i < rows.Len(); i++ {
		name := rows.Index(i).FieldByName("Name").String()
		if name == "" || seen[name] {
			t.Fatalf("empty or duplicate scenario name: %q", name)
		}
		seen[name] = true
	}
	return result
}

func equal[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func field(raw map[string]any, path ...string) (any, string) {
	var value any = raw
	for _, part := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, "invalid"
		}
		value, ok = object[part]
		if !ok {
			return nil, "unknown"
		}
	}
	return value, "present"
}

// projectFixture supplies resource metadata omitted by the field-focused milestone 1 fixtures.
// This is test setup only; production projection requires complete resource identity.
func projectFixture(t *testing.T, raw map[string]any, contract string) evidence.Cluster {
	t.Helper()
	object := make(map[string]any, len(raw)+3)
	for k, v := range raw {
		object[k] = v
	}
	metadata := map[string]any{"name": "bao", "namespace": "team", "uid": "cluster-1"}
	if existing, ok := raw["metadata"].(map[string]any); ok {
		for k, v := range existing {
			metadata[k] = v
		}
	}
	object["apiVersion"], object["kind"], object["metadata"] = "openbao.org/v1alpha1", "OpenBaoCluster", metadata
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	selected := adapter.Contract(contract)
	if contract == "unknown" {
		selected = adapter.OpenBaoV1Alpha1Unqualified
	}
	result, err := adapter.ProjectCluster(data, adapter.ProjectionOptions{
		Origin: "contract-cluster", CollectedAt: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		Contract: selected, Limits: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func fixtureNumber(value evidence.Integer) number {
	state := string(value.State)
	if value.State == evidence.ValueImplied {
		state = "implied_zero"
	}
	return number{State: state, Value: value.Value}
}

func readiness(t *testing.T, raw map[string]any, contract string) number {
	t.Helper()
	return fixtureNumber(projectFixture(t, raw, contract).ReadyReplicas)
}

func paused(t *testing.T, raw map[string]any, contract string) string {
	t.Helper()
	value := projectFixture(t, raw, contract).Paused
	if value.State == evidence.ValueImplied {
		return "implied_false"
	}
	if value.State != evidence.ValueReported {
		return string(value.State)
	}
	if *value.Value {
		return "reported_true"
	}
	return "reported_false"
}

func TestMissingValues(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name     string         `json:"name"`
		Contract string         `json:"contract"`
		Raw      map[string]any `json:"raw"`
		Ready    number         `json:"ready"`
		Paused   string         `json:"paused"`
	}](t, "values")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			equal(t, readiness(t, tc.Raw, tc.Contract), tc.Ready)
			equal(t, paused(t, tc.Raw, tc.Contract), tc.Paused)
		})
	}
}

type condition struct {
	Type       string `json:"type"`
	Status     string `json:"status"`
	Generation *int64 `json:"observedGeneration,omitempty"`
}

func conditions(t *testing.T, raw map[string]any) ([]condition, string) {
	t.Helper()
	observed := projectFixture(t, raw, "unknown").Conditions
	if observed.State != evidence.ValueReported {
		return nil, string(observed.State)
	}
	values := []condition{}
	for _, c := range observed.Values {
		values = append(values, condition{Type: c.Type, Status: string(c.Status), Generation: c.ObservedGeneration.Value})
	}
	return values, string(observed.State)
}

func TestConditions(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name  string         `json:"name"`
		Raw   map[string]any `json:"raw"`
		State string         `json:"state"`
		Want  []condition    `json:"want"`
		Ready number         `json:"ready"`
	}](t, "conditions")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, state := conditions(t, tc.Raw)
			equal(t, state, tc.State)
			equal(t, got, tc.Want)
			// Malformed conditions do not discard independent, valid scalar observations.
			equal(t, readiness(t, tc.Raw, "unknown"), tc.Ready)
		})
	}
}
