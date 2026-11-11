package adapter_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

const (
	poolLabel     = "openbao.org/workload-pool"
	revisionLabel = "openbao.org/revision"
)

func poolFixture(t *testing.T, kind string, change func(map[string]any)) []byte {
	t.Helper()
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(workloadFixture(t, kind)))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		t.Fatal(err)
	}
	change(root)
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func podPoolMetadata(t *testing.T, raw []byte) evidence.WorkloadMetadata {
	t.Helper()
	pod, err := adapter.ProjectPod(raw, workloadOptions(adapter.KubernetesCoreV1))
	if err != nil {
		t.Fatal(err)
	}
	return pod.Metadata
}

func TestPodPoolAndRevisionProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		labels   any
		present  bool
		pool     evidence.ValueState
		revision evidence.ValueState
	}{
		{"missing", nil, false, evidence.ValueUnknown, evidence.ValueUnknown},
		{"empty map", map[string]any{}, true, evidence.ValueUnknown, evidence.ValueUnknown},
		{"voter", map[string]any{poolLabel: "voter", revisionLabel: "blue-1"}, true, evidence.ValueReported, evidence.ValueReported},
		{"reader", map[string]any{poolLabel: "read-replica"}, true, evidence.ValueReported, evidence.ValueUnknown},
		{"null map", nil, true, evidence.ValueInvalid, evidence.ValueInvalid},
		{"invalid map", []any{"CANARY"}, true, evidence.ValueInvalid, evidence.ValueInvalid},
		{"null pool", map[string]any{poolLabel: nil, revisionLabel: "blue-1"}, true, evidence.ValueInvalid, evidence.ValueReported},
		{"empty pool", map[string]any{poolLabel: "", revisionLabel: "blue-1"}, true, evidence.ValueInvalid, evidence.ValueReported},
		{"unknown pool", map[string]any{poolLabel: "CANARY"}, true, evidence.ValueInvalid, evidence.ValueUnknown},
		{"null revision", map[string]any{poolLabel: "voter", revisionLabel: nil}, true, evidence.ValueReported, evidence.ValueInvalid},
		{"empty revision", map[string]any{poolLabel: "voter", revisionLabel: ""}, true, evidence.ValueReported, evidence.ValueInvalid},
		{
			"malformed revision",
			map[string]any{poolLabel: "voter", revisionLabel: map[string]any{"detail": "CANARY"}},
			true, evidence.ValueReported, evidence.ValueInvalid,
		},
		{"long revision", map[string]any{revisionLabel: strings.Repeat("a", 64)}, true, evidence.ValueUnknown, evidence.ValueInvalid},
		{"invalid revision", map[string]any{revisionLabel: "CANARY/invalid"}, true, evidence.ValueUnknown, evidence.ValueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := poolFixture(t, "pod", func(root map[string]any) {
				metadata := root["metadata"].(map[string]any)
				if tc.present {
					metadata["labels"] = tc.labels
				}
			})
			metadata := podPoolMetadata(t, raw)
			if metadata.Pool.State != tc.pool || metadata.Revision.State != tc.revision || metadata.Validate() != nil {
				t.Fatalf("pool/revision states = %s/%s, want %s/%s", metadata.Pool.State, metadata.Revision.State, tc.pool, tc.revision)
			}
			assertPoolReplay(t, metadata)
		})
	}
}

func assertPoolReplay(t *testing.T, metadata evidence.WorkloadMetadata) {
	t.Helper()
	serialized, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(serialized, []byte("CANARY")) {
		t.Fatal("unapproved source data entered metadata evidence")
	}
	var replay evidence.WorkloadMetadata
	if err := json.Unmarshal(serialized, &replay); err != nil || replay.Validate() != nil || !reflect.DeepEqual(replay, metadata) {
		t.Fatalf("pool/revision metadata did not survive replay: %v", err)
	}
}

func TestStatefulSetPoolSourcesRemainSeparate(t *testing.T) {
	t.Parallel()
	raw := poolFixture(t, "statefulset", func(root map[string]any) {
		root["metadata"].(map[string]any)["labels"] = map[string]any{poolLabel: "read-replica", revisionLabel: "TOP_LEVEL_CANARY"}
		root["spec"] = map[string]any{
			"replicas": 3,
			"selector": map[string]any{"matchLabels": map[string]any{poolLabel: "voter", revisionLabel: "blue-1"}},
			"template": map[string]any{"metadata": map[string]any{
				"labels": map[string]any{poolLabel: "read-replica", revisionLabel: "green-1", "unapproved": "CANARY"},
			}},
		}
	})
	set, err := adapter.ProjectStatefulSet(raw, workloadOptions(adapter.KubernetesAppsV1))
	if err != nil {
		t.Fatal(err)
	}
	if set.Metadata.Pool.State != evidence.ValueUnknown || set.Metadata.Revision.State != evidence.ValueUnknown ||
		set.SelectorPool.Value == nil || *set.SelectorPool.Value != "voter" ||
		set.SelectorRevision.Value == nil || *set.SelectorRevision.Value != "blue-1" ||
		set.TemplatePool.Value == nil || *set.TemplatePool.Value != "read-replica" ||
		set.TemplateRevision.Value == nil || *set.TemplateRevision.Value != "green-1" {
		t.Fatal("selector/template sources were merged, inferred, or replaced by top-level labels")
	}
	assertSafeReplay(t, set)
}

func TestStatefulSetPoolProjectionRetainsIndependentSources(t *testing.T) {
	t.Parallel()
	labels := map[string]any{poolLabel: "voter", revisionLabel: "blue-1"}
	selector := map[string]any{"matchLabels": labels}
	template := map[string]any{"metadata": map[string]any{"labels": labels}}
	for _, tc := range []struct {
		name          string
		selector      any
		template      any
		selectorState evidence.ValueState
		templateState evidence.ValueState
	}{
		{"missing labels", map[string]any{}, template, evidence.ValueUnknown, evidence.ValueReported},
		{"null selector labels", map[string]any{"matchLabels": nil}, template, evidence.ValueInvalid, evidence.ValueReported},
		{"malformed selector", "CANARY", template, evidence.ValueInvalid, evidence.ValueReported},
		{
			"null template labels", selector,
			map[string]any{"metadata": map[string]any{"labels": nil}},
			evidence.ValueReported, evidence.ValueInvalid,
		},
		{"malformed template", selector, "CANARY", evidence.ValueReported, evidence.ValueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := poolFixture(t, "statefulset", func(root map[string]any) {
				spec := root["spec"].(map[string]any)
				spec["selector"] = tc.selector
				spec["template"] = tc.template
			})
			set, err := adapter.ProjectStatefulSet(raw, workloadOptions(adapter.KubernetesAppsV1))
			if err != nil {
				t.Fatal(err)
			}
			if set.SelectorPool.State != tc.selectorState || set.SelectorRevision.State != tc.selectorState ||
				set.TemplatePool.State != tc.templateState || set.TemplateRevision.State != tc.templateState {
				t.Fatal("malformed source discarded independent selector/template facts")
			}
			assertSafeReplay(t, set)
		})
	}
}

func TestStatefulSetGroupingReplayRejectsForgedValues(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		"selectorPool", "templatePool", "selectorRevision", "templateRevision", "metadataPool", "metadataRevision",
	} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			set := workloadSet(t)
			serialized, err := json.Marshal(set)
			if err != nil {
				t.Fatal(err)
			}
			var replay evidence.StatefulSet
			if err := json.Unmarshal(serialized, &replay); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "selectorPool":
				replay.SelectorPool = poolReportedText("unsupported")
			case "templatePool":
				replay.TemplatePool = poolReportedText("unsupported")
			case "selectorRevision":
				replay.SelectorRevision = poolReportedText(strings.Repeat("a", 64))
			case "templateRevision":
				replay.TemplateRevision = poolReportedText(strings.Repeat("a", 64))
			case "metadataPool":
				replay.Metadata.Pool = poolReportedText("voter")
			case "metadataRevision":
				replay.Metadata.Revision = poolReportedText("blue-1")
			}
			if replay.Validate() == nil {
				t.Fatal("normalized replay accepted a forged grouping value")
			}
		})
	}
}

func poolReportedText(value string) evidence.Text {
	return evidence.Text{State: evidence.ValueReported, Value: &value}
}

func TestPoolLabelsNeverConfirmOwnership(t *testing.T) {
	t.Parallel()
	raw := poolFixture(t, "statefulset", func(root map[string]any) {
		metadata := root["metadata"].(map[string]any)
		delete(metadata, "ownerReferences")
		root["spec"] = map[string]any{
			"replicas": 3,
			"selector": map[string]any{"matchLabels": map[string]any{poolLabel: "voter"}},
			"template": map[string]any{"metadata": map[string]any{"labels": map[string]any{poolLabel: "voter"}}},
		}
	})
	set, err := adapter.ProjectStatefulSet(raw, workloadOptions(adapter.KubernetesAppsV1))
	if err != nil {
		t.Fatal(err)
	}
	association, err := adapter.AssociateStatefulSet(set, clusterIdentity())
	if err != nil || association.State != evidence.AssociationUnconfirmed || association.Basis != "" {
		t.Fatal("pool labels or reserved annotations stood in for a controller UID")
	}
}
