package contracttest

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// All models in this directory are executable specifications, not public library APIs.
const adapterContract = "openbao.org/v1alpha1@9671c306"

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

func integer(raw map[string]any, path ...string) number {
	value, state := field(raw, path...)
	if state != "present" {
		return number{State: state}
	}
	f, ok := value.(json.Number)
	if !ok {
		return number{State: "invalid"}
	}
	n, err := f.Int64()
	if err != nil {
		return number{State: "invalid"}
	}
	return number{State: "reported", Value: &n}
}

func count(raw map[string]any, path ...string) number {
	result := integer(raw, path...)
	if result.Value != nil && (*result.Value < 0 || *result.Value > 2147483647) {
		return number{State: "invalid"}
	}
	return result
}

func readiness(raw map[string]any, contract string) number {
	ready := count(raw, "status", "readyReplicas")
	if ready.State != "unknown" || contract != adapterContract {
		return ready
	}
	generation, observed := integer(raw, "metadata", "generation"), integer(raw, "status", "observedGeneration")
	desired := count(raw, "spec", "replicas")
	if desired.State != "reported" || evidence.CompareGeneration(generation.Value, observed.Value) != evidence.GenerationCurrent {
		return ready
	}
	zero := int64(0)
	return number{State: "implied_zero", Value: &zero}
}

func paused(raw map[string]any, contract string) string {
	parent, parentState := field(raw, "spec")
	if parentState != "present" {
		return parentState
	}
	if _, ok := parent.(map[string]any); !ok {
		return "invalid"
	}
	value, state := field(raw, "spec", "paused")
	if state == "unknown" && contract == adapterContract {
		return "implied_false"
	}
	if state != "present" {
		return state
	}
	if value, ok := value.(bool); ok {
		if value {
			return "reported_true"
		}
		return "reported_false"
	}
	return "invalid"
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
			equal(t, readiness(tc.Raw, tc.Contract), tc.Ready)
			equal(t, paused(tc.Raw, tc.Contract), tc.Paused)
		})
	}
}

type condition struct {
	Type       string `json:"type"`
	Status     string `json:"status"`
	Generation *int64 `json:"observedGeneration,omitempty"`
}

func conditions(raw map[string]any) ([]condition, string) {
	value, state := field(raw, "status", "conditions")
	if state != "present" {
		return nil, state
	}
	items, ok := value.([]any)
	if !ok {
		return nil, "invalid"
	}
	result := []condition{}
	seen := map[string]bool{}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, "invalid"
		}
		name, nameOK := object["type"].(string)
		status, statusOK := object["status"].(string)
		if !nameOK || !statusOK || name == "" || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) || seen[name] ||
			!slices.Contains([]string{"True", "False", "Unknown"}, status) {
			return nil, "invalid"
		}
		seen[name] = true
		observed := integer(object, "observedGeneration")
		if observed.State == "invalid" {
			return nil, "invalid"
		}
		result = append(result, condition{Type: name, Status: status, Generation: observed.Value})
	}
	slices.SortFunc(result, func(a, b condition) int { return strings.Compare(a.Type, b.Type) })
	return result, "reported"
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
			got, state := conditions(tc.Raw)
			equal(t, state, tc.State)
			equal(t, got, tc.Want)
			// Malformed conditions do not discard independent, valid scalar observations.
			equal(t, readiness(tc.Raw, "unknown"), tc.Ready)
		})
	}
}
