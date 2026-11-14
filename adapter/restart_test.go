package adapter_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestRestartProjectionPreservesPresenceAndRejectsMalformedFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, runtime string
		object        evidence.ObjectState
		field         evidence.ValueState
	}{
		{"absent", "", evidence.ObjectAbsent, evidence.ValueUnknown},
		{"null", `,"runtime":null`, evidence.ObjectNull, evidence.ValueUnknown},
		{"object", `,"runtime":{"restartAt":"2026-11-14T00:00:00Z","provider":"CANARY"}`, evidence.ObjectReported, evidence.ValueReported},
		{"empty", `,"runtime":{}`, evidence.ObjectReported, evidence.ValueUnknown},
		{"invalid-object", `,"runtime":false`, evidence.ObjectInvalid, evidence.ValueUnknown},
		{"invalid-marker", `,"runtime":{"restartAt":null}`, evidence.ObjectReported, evidence.ValueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(
				`{"apiVersion":"kubebao.org/v1alpha1",
"kind":"OpenBaoCluster",
"metadata":{"namespace":"team",
"name":"bao",
"uid":"c",
"generation":1},
"spec":{"replicas":1,
"upgrade":{"strategy":"RollingUpdate"}%s},
"status":{"observedGeneration":1,
"acceptedUpgradeStrategy":"RollingUpdate"}}`,
				tc.runtime,
			)
			opts := projectionOptions()
			opts.Contract = adapter.KubeBaoV1Alpha1
			c, e := adapter.ProjectCluster([]byte(raw), opts)
			if e != nil {
				t.Fatal(e)
			}
			if c.Runtime.State != tc.object || c.Runtime.RestartAt.State != tc.field || c.UpgradeStrategy.Value == nil ||
				c.AcceptedUpgradeStrategy.Value == nil {
				t.Fatalf("unexpected runtime %+v", c.Runtime)
			}
			body, e := json.Marshal(c)
			if e != nil || strings.Contains(string(body), "CANARY") {
				t.Fatal("provider field retained")
			}
		})
	}
}

func TestRestartWorkloadProjectionUsesTemplateAndPodFields(t *testing.T) {
	t.Parallel()
	opts := projectionOptions()
	opts.Contract = adapter.KubernetesAppsV1
	raw := `{"apiVersion":"apps/v1",
"kind":"StatefulSet",
"metadata":{"namespace":"team",
"name":"bao",
"uid":"set",
"generation":2,
"annotations":{"openbao.org/restart-at":"WRONG_TOP_LEVEL"}},
"spec":{"replicas":1,
"minReadySeconds":10,
"updateStrategy":{"type":"RollingUpdate",
"rollingUpdate":{"partition":0}},
"template":{"metadata":{"annotations":{"openbao.org/restart-at":"marker",
"provider":"CANARY"}}}},
"status":{"observedGeneration":2,
"currentRevision":"bao-hash",
"updateRevision":"bao-hash",
"readyReplicas":1,
"currentReplicas":1,
"updatedReplicas":1,
"availableReplicas":0}}`
	set, e := adapter.ProjectStatefulSet([]byte(raw), opts)
	if e != nil {
		t.Fatal(e)
	}
	if set.Metadata.RestartAt.State != evidence.ValueUnknown {
		t.Fatal("StatefulSet top-level marker was projected")
	}
	if set.TemplateRestartAt.Value == nil || *set.TemplateRestartAt.Value != "marker" ||
		set.UpdateStrategy.Value == nil || *set.UpdateStrategy.Value != "RollingUpdate" ||
		set.RollingPartition.Value == nil ||
		*set.RollingPartition.Value != 0 ||
		set.MinReadySeconds.Value == nil ||
		*set.MinReadySeconds.Value != 10 ||
		set.AvailableReplicas.Value == nil ||
		*set.AvailableReplicas.Value != 0 {
		t.Fatalf("incorrect STS restart projection %+v", set)
	}
	for _, part := range []string{`"partition":null`, `"partition":-1`, `"partition":0.5`} {
		changed := strings.Replace(raw, `"partition":0`, part, 1)
		s, e := adapter.ProjectStatefulSet([]byte(changed), opts)
		if e != nil || s.RollingPartition.State != evidence.ValueInvalid {
			t.Fatal("invalid partition accepted")
		}
	}
	opts.Contract = adapter.KubernetesCoreV1
	pod, e := adapter.ProjectPod(
		[]byte(
			`{"apiVersion":"v1",
"kind":"Pod",
"metadata":{"namespace":"team",
"name":"bao-0",
"uid":"pod",
"annotations":{"openbao.org/restart-at":"marker",
"provider":"CANARY"},
"labels":{"controller-revision-hash":"bao-hash"}}}`,
		),
		opts,
	)
	if e != nil || pod.Metadata.RestartAt.Value == nil || *pod.Metadata.RestartAt.Value != "marker" ||
		pod.Metadata.ControllerRevision.Value == nil ||
		*pod.Metadata.ControllerRevision.Value != "bao-hash" {
		t.Fatal("Pod restart projection failed", e)
	}
	body, e := json.Marshal([]any{set, pod})
	if e != nil || strings.Contains(string(body), "CANARY") || strings.Contains(string(body), "WRONG_TOP_LEVEL") {
		t.Fatal("unapproved metadata retained")
	}
}
