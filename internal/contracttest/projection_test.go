package contracttest

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/kubebao/openbao-operator-client/adapter"
)

type safeProjection struct {
	Ready          number      `json:"ready"`
	Conditions     []condition `json:"conditions"`
	ConditionState string      `json:"conditionState"`
	Index          *string     `json:"index"`
	ContainerCode  *int64      `json:"containerExitCode"`
}

func project(t *testing.T, raw map[string]any, contract string) safeProjection {
	result := safeProjection{Ready: readiness(t, raw, contract)}
	result.Conditions, result.ConditionState = conditions(t, raw)
	if value, state := field(raw, "status", "index"); state == "present" {
		if text, ok := value.(string); ok {
			parsed, err := strconv.ParseUint(text, 10, 64)
			if err == nil && strconv.FormatUint(parsed, 10) == text {
				result.Index = &text
			}
		}
	}
	result.ContainerCode = projectedContainerCode(t, raw)
	return result
}

// The draft scalar is placed in its public Pod field before production projection.
// Remote indexes above remain a future specification: the pinned operator has no remote API.
func projectedContainerCode(t *testing.T, raw map[string]any) *int64 {
	t.Helper()
	code, state := field(raw, "status", "containerExitCode")
	if state != "present" {
		return nil
	}
	object := map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"namespace": "team", "name": "bao-0", "uid": "pod-1"},
		"status": map[string]any{"containerStatuses": []any{map[string]any{
			"name": "bao", "state": map[string]any{"terminated": map[string]any{"exitCode": code}},
		}}},
	}
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	value, err := adapter.ProjectPod(data, fixtureOptions("origin", adapter.KubernetesCoreV1))
	if err != nil || len(value.Containers.Values) != 1 {
		t.Fatal("container fixture could not be projected")
	}
	return value.Containers.Values[0].ExitCode.Value
}

// Rich text is a separate local sidecar. It never becomes part of serializable evidence.
func localText(fields map[string]string, enabled bool) map[string]string {
	result := map[string]string{}
	if !enabled {
		return result
	}
	allowed := []string{"condition.message", "container.waiting.message", "restore.message"}
	remaining := 512
	for _, key := range allowed {
		text, ok := fields[key]
		if !ok || remaining == 0 {
			continue
		}
		text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, strings.ToValidUTF8(text, ""))
		limit := min(256, remaining)
		if len(text) > limit {
			text = text[:limit]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		result[key] = text
		remaining -= len(text)
	}
	return result
}

func TestSafeProjection(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name     string         `json:"name"`
		Raw      map[string]any `json:"raw"`
		Contract string         `json:"contract"`
		Canaries []string       `json:"canaries"`
		Want     safeProjection `json:"want"`
	}](t, "projection")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := project(t, tc.Raw, tc.Contract)
			equal(t, got, tc.Want)
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			full, err := json.Marshal(projectFixture(t, tc.Raw, tc.Contract))
			if err != nil {
				t.Fatal(err)
			}
			for _, canary := range tc.Canaries {
				if strings.Contains(string(encoded), canary) || strings.Contains(string(full), canary) {
					t.Fatalf("provider text retained: %s", canary)
				}
			}
			var fields map[string]any
			if err := json.Unmarshal(full, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"exitCode", "resultCode", "failureThreshold"} {
				if _, exists := fields[key]; exists {
					t.Fatalf("process policy in evidence: %s", key)
				}
			}
		})
	}
}

func TestLocalTextPolicy(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name    string            `json:"name"`
		Enabled bool              `json:"enabled"`
		Fields  map[string]string `json:"fields"`
		Want    map[string]string `json:"want"`
	}](t, "text")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, localText(tc.Fields, tc.Enabled), tc.Want) })
	}
	long := strings.Repeat("界", 100)
	got := localText(map[string]string{
		"condition.message": long, "container.waiting.message": long, "restore.message": long, "unknown": "CANARY",
	}, true)
	equal(t, len(got["condition.message"]), 255)
	equal(t, len(got["container.waiting.message"]), 255)
	equal(t, got["restore.message"], "")
	for _, text := range got {
		if !utf8.ValidString(text) {
			t.Fatal("truncated UTF-8")
		}
	}
	keys := []string{}
	for key := range got {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	equal(t, keys, []string{"condition.message", "container.waiting.message", "restore.message"})
}
