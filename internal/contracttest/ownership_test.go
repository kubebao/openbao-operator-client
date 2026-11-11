package contracttest

import (
	"encoding/json"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

type ownerReference struct {
	Group      string `json:"group"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller bool   `json:"controller"`
}

func fixtureWorkload(t *testing.T, id identity, refs []ownerReference, annotation string) []byte {
	t.Helper()
	owners := []any{}
	for _, ref := range refs {
		owners = append(
			owners,
			map[string]any{
				"apiVersion": fixtureAPIVersion(ref.Group, "v1"),
				"kind":       ref.Kind,
				"name":       ref.Name,
				"uid":        ref.UID,
				"controller": ref.Controller,
			},
		)
	}
	metadata := map[string]any{"name": id.Name, "namespace": id.Namespace, "uid": id.UID, "ownerReferences": owners}
	if annotation != "" {
		metadata["annotations"] = map[string]any{"openbao.org/owner-uid": annotation}
	}
	data, err := json.Marshal(map[string]any{"apiVersion": fixtureAPIVersion(id.Group, "v1"), "kind": id.Kind, "metadata": metadata})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWorkloadOwnership(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name       string           `json:"name"`
		Cluster    identity         `json:"cluster"`
		Set        identity         `json:"set"`
		Pod        identity         `json:"pod"`
		SetOwners  []ownerReference `json:"setOwners"`
		PodOwners  []ownerReference `json:"podOwners"`
		Annotation string           `json:"annotation"`
		SetState   string           `json:"setState"`
		PodState   string           `json:"podState"`
	}](t, "ownership")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			set, err := adapter.ProjectStatefulSet(
				fixtureWorkload(t, tc.Set, tc.SetOwners, tc.Annotation),
				fixtureOptions(tc.Set.Origin, adapter.KubernetesAppsV1),
			)
			if err != nil {
				t.Fatal(err)
			}
			pod, err := adapter.ProjectPod(
				fixtureWorkload(t, tc.Pod, tc.PodOwners, ""),
				fixtureOptions(tc.Pod.Origin, adapter.KubernetesCoreV1),
			)
			if err != nil {
				t.Fatal(err)
			}
			setAssociation, err := adapter.AssociateStatefulSet(set, evidence.Identity(tc.Cluster))
			if err != nil {
				t.Fatal(err)
			}
			equal(t, string(setAssociation.State), tc.SetState)
			podAssociation, err := adapter.AssociatePod(pod, set, setAssociation)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, string(podAssociation.State), tc.PodState)
		})
	}
}
