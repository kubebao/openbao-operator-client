package adapter_test

import (
	"bytes"
	"embed"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

//go:embed testdata/statefulset.json testdata/pod.json
var workloadFiles embed.FS

func workloadFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := workloadFiles.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func workloadOptions(contract adapter.Contract) adapter.ProjectionOptions {
	options := projectionOptions()
	options.Contract = contract
	return options
}

func workloadSet(t *testing.T) evidence.StatefulSet {
	t.Helper()
	value, err := adapter.ProjectStatefulSet(workloadFixture(t, "statefulset"), workloadOptions(adapter.KubernetesAppsV1))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func workloadPod(t *testing.T) evidence.Pod {
	t.Helper()
	value, err := adapter.ProjectPod(workloadFixture(t, "pod"), workloadOptions(adapter.KubernetesCoreV1))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func clusterIdentity() evidence.Identity {
	return evidence.Identity{
		Origin:    "origin-1",
		Group:     "openbao.org",
		Kind:      "OpenBaoCluster",
		Namespace: "team",
		Name:      "bao",
		UID:       "cluster-1",
	}
}

func TestWorkloadProjectionAndSafeReplay(t *testing.T) {
	t.Parallel()
	set, pod := workloadSet(t), workloadPod(t)
	if set.Acknowledgement() != evidence.GenerationCurrent || set.ReadyReplicas.State != evidence.ValueImplied ||
		*set.ReadyReplicas.Value != 0 {
		t.Fatal("known current omitted StatefulSet readiness was not recorded as implied zero")
	}
	if *set.Metadata.Maintenance.Value != "false" || *pod.Metadata.Maintenance.Value != "true" {
		t.Fatal("maintenance annotation facts lost")
	}
	if pod.Conditions.Values[0].Status != evidence.ConditionFalse || *pod.Containers.Values[0].WaitingReason.Value != "CrashLoopBackOff" ||
		*pod.Containers.Values[1].ExitCode.Value != 1073741819 || *pod.InitContainers.Values[0].ExitCode.Value != 0 {
		t.Fatal("container states or termination codes were lost or reordered")
	}
	if pod.Containers.Values[0].Name != "bao" || pod.Containers.Values[1].Name != "worker" {
		t.Fatal("container ordering is unstable")
	}
	assertSafeReplay(t, set)
	assertSafeReplay(t, pod)
}

func TestStatefulSetOmissionNeedsCurrentAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spec, status string
		want               evidence.ValueState
	}{
		{"current", `{"replicas":3}`, `{"observedGeneration":4}`, evidence.ValueImplied},
		{"missing", `{"replicas":3}`, `{}`, evidence.ValueUnknown},
		{"zero", `{"replicas":3}`, `{"observedGeneration":0}`, evidence.ValueUnknown},
		{"stale", `{"replicas":3}`, `{"observedGeneration":3}`, evidence.ValueUnknown},
		{"future", `{"replicas":3}`, `{"observedGeneration":5}`, evidence.ValueUnknown},
		{"missing-desired", `{}`, `{"observedGeneration":4}`, evidence.ValueUnknown},
		{"null-ready", `{"replicas":3}`, `{"observedGeneration":4,"readyReplicas":null}`, evidence.ValueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := replaceSection(t, workloadFixture(t, "statefulset"), "spec", tc.spec)
			data = replaceSection(t, data, "status", tc.status)
			set, err := adapter.ProjectStatefulSet(data, workloadOptions(adapter.KubernetesAppsV1))
			if err != nil {
				t.Fatal(err)
			}
			if set.ReadyReplicas.State != tc.want {
				t.Fatalf("ready state = %s, want %s", set.ReadyReplicas.State, tc.want)
			}
		})
	}
}

func replaceSection(t *testing.T, data []byte, key, raw string) []byte {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	object[key] = json.RawMessage(raw)
	result, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestNullableAnnotationMapAndInvalidBindings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, annotations string
		want              evidence.ValueState
		association       evidence.AssociationState
	}{
		{"null-map", `null`, evidence.ValueUnknown, evidence.AssociationConfirmed},
		{"empty-map", `{}`, evidence.ValueUnknown, evidence.AssociationConfirmed},
		{"malformed-map", `"MALFORMED_CANARY"`, evidence.ValueInvalid, evidence.AssociationUnconfirmed},
		{"null-binding", `{"openbao.org/owner-uid":null}`, evidence.ValueInvalid, evidence.AssociationUnconfirmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(workloadFixture(t, "statefulset"), &object); err != nil {
				t.Fatal(err)
			}
			metadata := replaceSection(t, object["metadata"], "annotations", tc.annotations)
			data := replaceSection(t, workloadFixture(t, "statefulset"), "metadata", string(metadata))
			set, err := adapter.ProjectStatefulSet(data, workloadOptions(adapter.KubernetesAppsV1))
			if err != nil {
				t.Fatal(err)
			}
			association, err := adapter.AssociateStatefulSet(set, clusterIdentity())
			if err != nil {
				t.Fatal(err)
			}
			if set.Metadata.ClusterOwnerUID.State != tc.want || association.State != tc.association {
				t.Fatalf("annotation state = %s, association = %s", set.Metadata.ClusterOwnerUID.State, association.State)
			}
			assertSafeReplay(t, set)
		})
	}
}

func TestRestoreAnnotationsPreserveOpaqueIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		annotations string
		want        evidence.ValueState
	}{
		{`null`, evidence.ValueUnknown},
		{`"MALFORMED_CANARY"`, evidence.ValueInvalid},
		{`{"openbao.org/restore-origin":null}`, evidence.ValueInvalid},
		{`{"openbao.org/restore-origin":"request:1"}`, evidence.ValueReported},
	} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(clusterFixture(t), &object); err != nil {
			t.Fatal(err)
		}
		metadata := replaceSection(t, object["metadata"], "annotations", tc.annotations)
		data := replaceSection(t, clusterFixture(t), "metadata", string(metadata))
		cluster, err := adapter.ProjectCluster(data, projectionOptions())
		if err != nil {
			t.Fatal(err)
		}
		if cluster.RestoreOrigin.State != tc.want ||
			tc.want == evidence.ValueReported && *cluster.RestoreOrigin.Value != "request:1" {
			t.Fatal("restore annotation identity was defaulted, sanitized, or discarded")
		}
		assertSafeReplay(t, cluster)
	}
}

func TestControllerChainChecksBothUIDLevels(t *testing.T) {
	t.Parallel()
	set, pod := workloadSet(t), workloadPod(t)
	parent, err := adapter.AssociateStatefulSet(set, clusterIdentity())
	if err != nil {
		t.Fatal(err)
	}
	child, err := adapter.AssociatePod(pod, set, parent)
	if err != nil {
		t.Fatal(err)
	}
	if parent.State != evidence.AssociationConfirmed || child.State != evidence.AssociationConfirmed || child.Target.UID != "set-1" {
		t.Fatal("valid cluster/StatefulSet/Pod chain was not confirmed")
	}
	wrong := "set-1"
	pod.Metadata.ClusterOwnerUID = evidence.Text{State: evidence.ValueReported, Value: &wrong}
	child, err = adapter.AssociatePod(pod, set, parent)
	if err != nil {
		t.Fatal(err)
	}
	if child.State != evidence.AssociationConflict {
		t.Fatal("cluster annotation was compared to the immediate parent UID")
	}
	pod = workloadPod(t)
	set.Metadata.Reference.Identity.UID = "set-2"
	child, err = adapter.AssociatePod(pod, set, parent)
	if err != nil {
		t.Fatal(err)
	}
	if child.State != evidence.AssociationUnconfirmed {
		t.Fatal("replacement reused the old parent association")
	}
	parent, err = adapter.AssociateStatefulSet(set, clusterIdentity())
	if err != nil {
		t.Fatal(err)
	}
	child, err = adapter.AssociatePod(pod, set, parent)
	if err != nil {
		t.Fatal(err)
	}
	if child.State != evidence.AssociationForeign {
		t.Fatal("old Pod owner UID applied to a replacement set")
	}
}

func TestOwnerMetadataCannotBeSelectedByName(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`[]`,
		`[{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster","name":"bao","uid":"cluster-1"}]`,
		`[{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster","name":"bao","uid":"cluster-1","controller":"true"}]`,
		`[{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster","name":"bao","uid":"cluster-1","controller":true},` +
			`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster","name":"bao","uid":"other","controller":true}]`,
	} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(workloadFixture(t, "statefulset"), &object); err != nil {
			t.Fatal(err)
		}
		var metadata map[string]json.RawMessage
		if err := json.Unmarshal(object["metadata"], &metadata); err != nil {
			t.Fatal(err)
		}
		metadata["ownerReferences"] = json.RawMessage(raw)
		encoded, err := json.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		object["metadata"] = encoded
		data, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		set, err := adapter.ProjectStatefulSet(data, workloadOptions(adapter.KubernetesAppsV1))
		if err != nil {
			t.Fatal(err)
		}
		association, err := adapter.AssociateStatefulSet(set, clusterIdentity())
		if err != nil {
			t.Fatal(err)
		}
		if association.State != evidence.AssociationUnconfirmed {
			t.Fatal("missing/ambiguous controller claim established ownership")
		}
	}
}

func TestControllerReferenceVersionDoesNotChangeIdentity(t *testing.T) {
	t.Parallel()
	data := bytes.Replace(workloadFixture(t, "statefulset"), []byte("openbao.org/v1alpha1"), []byte("openbao.org/v1beta1"), 1)
	set, err := adapter.ProjectStatefulSet(data, workloadOptions(adapter.KubernetesAppsV1))
	if err != nil {
		t.Fatal(err)
	}
	association, err := adapter.AssociateStatefulSet(set, clusterIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if association.State != evidence.AssociationConfirmed {
		t.Fatal("served version changed stable owner identity")
	}
}

func TestWorkloadFieldBounds(t *testing.T) {
	t.Parallel()
	options := workloadOptions(adapter.KubernetesCoreV1)
	options.Limits.MaxEntries = 1
	pod, err := adapter.ProjectPod(workloadFixture(t, "pod"), options)
	if err != nil {
		t.Fatal(err)
	}
	if pod.Containers.State != evidence.ValueInvalid || pod.InitContainers.State != evidence.ValueReported ||
		pod.Conditions.State != evidence.ValueReported {
		t.Fatal("container bound discarded independent fields")
	}
	data := bytes.Replace(workloadFixture(t, "pod"), []byte(`"exitCode":1073741819`), []byte(`"exitCode":-2147483648`), 1)
	pod, err = adapter.ProjectPod(data, workloadOptions(adapter.KubernetesCoreV1))
	if err != nil {
		t.Fatal(err)
	}
	if *pod.Containers.Values[1].ExitCode.Value != -2147483648 {
		t.Fatal("signed termination code was lost")
	}
	data = bytes.Replace(data, []byte(`"exitCode":-2147483648`), []byte(`"exitCode":2147483648`), 1)
	pod, err = adapter.ProjectPod(data, workloadOptions(adapter.KubernetesCoreV1))
	if err != nil {
		t.Fatal(err)
	}
	if pod.Containers.Values[1].ExitCode.State != evidence.ValueInvalid || pod.Conditions.State != evidence.ValueReported {
		t.Fatal("bad termination code altered Pod readiness")
	}
}

func TestContractRestrictsKinds(t *testing.T) {
	t.Parallel()
	_, err := adapter.ProjectPod(workloadFixture(t, "pod"), workloadOptions(adapter.KubernetesAppsV1))
	assertProjectionError(t, err, adapter.UnsupportedContract)
	_, err = adapter.ProjectStatefulSet(workloadFixture(t, "statefulset"), projectionOptions())
	assertProjectionError(t, err, adapter.UnsupportedContract)
	_, err = adapter.ProjectCluster(clusterFixture(t), workloadOptions(adapter.KubernetesCoreV1))
	assertProjectionError(t, err, adapter.UnsupportedContract)
	info, err := adapter.Describe(adapter.KubernetesCoreV1)
	if err != nil {
		t.Fatal(err)
	}
	info.Kinds[0] = "changed"
	again, err := adapter.Describe(adapter.KubernetesCoreV1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Kinds, []string{"Pod"}) || again.KubernetesVersion != "v0.37.2" {
		t.Fatal("contract kind set was mutated")
	}
}

func TestMalformedRestoreBindingCannotBeIgnored(t *testing.T) {
	t.Parallel()
	target, err := adapter.ProjectCluster(clusterFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(restoreFixture(t), &object); err != nil {
		t.Fatal(err)
	}
	var status map[string]json.RawMessage
	if err := json.Unmarshal(object["status"], &status); err != nil {
		t.Fatal(err)
	}
	status["target"] = json.RawMessage(`"MALFORMED_CANARY"`)
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	object["status"] = encoded
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	request, err := adapter.ProjectRestore(data, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	association, err := adapter.AssociateRestore(target, request)
	if err != nil {
		t.Fatal(err)
	}
	if association.State != evidence.AssociationUnconfirmed {
		t.Fatal("matching execution UID hid an invalid target binding")
	}
}

func TestLatestRestoreRejectsDuplicateAndFutureRecords(t *testing.T) {
	t.Parallel()
	target, err := adapter.ProjectCluster(clusterFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	request, err := adapter.ProjectRestore(restoreFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = adapter.LatestRestore(target, []evidence.Restore{request, request})
	assertProjectionError(t, err, adapter.InvalidInput)
	future := projectionOptions().CollectedAt.AddDate(0, 1, 0)
	request.CreatedAt = evidence.Timestamp{State: evidence.ValueReported, Value: &future}
	_, found, err := adapter.LatestRestore(target, []evidence.Restore{request})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("future producer time selected a display request")
	}
}

func FuzzProjectWorkload(f *testing.F) {
	f.Add(workloadFixture(f, "pod"))
	f.Add(workloadFixture(f, "statefulset"))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		pod, podErr := adapter.ProjectPod(data, workloadOptions(adapter.KubernetesCoreV1))
		if podErr == nil {
			if pod.Validate() != nil {
				t.Fatal("invalid normalized Pod")
			}
			if _, err := json.Marshal(pod); err != nil {
				t.Fatal(err)
			}
		}
		set, setErr := adapter.ProjectStatefulSet(data, workloadOptions(adapter.KubernetesAppsV1))
		if setErr == nil {
			if set.Validate() != nil {
				t.Fatal("invalid normalized StatefulSet")
			}
			if _, err := json.Marshal(set); err != nil {
				t.Fatal(err)
			}
		}
	})
}
