package contracttest

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type identity evidence.Identity

type request struct {
	Identity         identity `json:"identity"`
	TargetName       string   `json:"targetName"`
	TargetUID        string   `json:"targetUID"`
	ExecutionUID     string   `json:"executionUID"`
	OriginRequestUID string   `json:"originRequestUID"`
	HoldOperationID  string   `json:"holdOperationID"`
	OperationID      string   `json:"operationID"`
	Contract         string   `json:"contract"`
	CreatedAt        string   `json:"createdAt"`
}

type association struct {
	State string `json:"state"`
	Basis string `json:"basis,omitempty"`
}

func fixtureTarget(t *testing.T, target identity, r request) (evidence.Cluster, error) {
	t.Helper()
	metadata := map[string]any{"name": target.Name, "namespace": target.Namespace, "uid": target.UID}
	annotations := map[string]any{}
	if r.OriginRequestUID != "" {
		annotations["openbao.org/restore-origin"] = r.OriginRequestUID
	}
	if r.HoldOperationID != "" {
		annotations["openbao.org/restore-hold"] = r.HoldOperationID
	}
	metadata["annotations"] = annotations
	data, err := json.Marshal(map[string]any{"apiVersion": "openbao.org/v1alpha1", "kind": "OpenBaoCluster", "metadata": metadata})
	if err != nil {
		t.Fatal(err)
	}
	options := fixtureOptions(target.Origin, adapter.V1Alpha1Snapshot)
	value, err := adapter.ProjectCluster(data, options)
	if err == nil {
		value.Reference.Identity = evidence.Identity(target)
		value.Reference.APIVersion = fixtureAPIVersion(target.Group, "v1alpha1")
	}
	return value, err
}

func fixtureOptions(origin string, contract adapter.Contract) adapter.ProjectionOptions {
	return adapter.ProjectionOptions{
		Origin:      origin,
		CollectedAt: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		Contract:    contract,
		Limits:      adapter.DefaultProjectionLimits(),
	}
}

func fixtureAPIVersion(group, version string) string {
	if group == "" {
		return version
	}
	return group + "/" + version
}

func fixtureRestore(t *testing.T, r request) (evidence.Restore, error) {
	t.Helper()
	metadata := map[string]any{"name": r.Identity.Name, "namespace": r.Identity.Namespace, "uid": r.Identity.UID}
	if r.CreatedAt != "" {
		metadata["creationTimestamp"] = r.CreatedAt
	}
	status := map[string]any{}
	if r.TargetUID != "" {
		status["target"] = map[string]any{"uid": r.TargetUID}
	}
	execution := map[string]any{}
	if r.ExecutionUID != "" {
		execution["targetUID"] = r.ExecutionUID
	}
	if r.OperationID != "" {
		execution["operationID"] = r.OperationID
	}
	if len(execution) > 0 {
		status["execution"] = execution
	}
	spec := map[string]any{}
	if r.TargetName != "" {
		spec["cluster"] = r.TargetName
	}
	data, err := json.Marshal(
		map[string]any{"apiVersion": "openbao.org/v1alpha1", "kind": r.Identity.Kind, "metadata": metadata, "spec": spec, "status": status},
	)
	if err != nil {
		t.Fatal(err)
	}
	contract := adapter.Contract(r.Contract)
	if r.Contract == "unknown" {
		contract = adapter.V1Alpha1Unqualified
	}
	value, err := adapter.ProjectRestore(data, fixtureOptions(r.Identity.Origin, contract))
	if err == nil {
		value.Reference.Identity = evidence.Identity(r.Identity)
		value.Reference.APIVersion = fixtureAPIVersion(r.Identity.Group, "v1alpha1")
	}
	return value, err
}

func associate(t *testing.T, target identity, r request) association {
	t.Helper()
	owner, err := fixtureTarget(t, target, r)
	if err != nil {
		return association{State: "invalid"}
	}
	if r.Identity.Kind != "OpenBaoRestore" {
		return association{State: "unconfirmed"}
	}
	request, err := fixtureRestore(t, r)
	if err != nil {
		return association{State: "invalid"}
	}
	result, err := adapter.AssociateRestore(owner, request)
	if err != nil {
		return association{State: "invalid"}
	}
	return association{State: string(result.State), Basis: string(result.Basis)}
}

func latest(t *testing.T, target identity, requests []request) string {
	t.Helper()
	owner, err := fixtureTarget(t, target, request{})
	if err != nil {
		t.Fatal(err)
	}
	projected := []evidence.Restore{}
	for _, r := range requests {
		value, err := fixtureRestore(t, r)
		if err != nil {
			t.Fatal(err)
		}
		projected = append(projected, value)
	}
	result, found, err := adapter.LatestRestore(owner, projected)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return ""
	}
	return result.Reference.Identity.UID
}

func TestAssociations(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name    string      `json:"name"`
		Target  identity    `json:"target"`
		Request request     `json:"request"`
		Want    association `json:"want"`
	}](t, "associations")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, associate(t, tc.Target, tc.Request), tc.Want) })
	}
}

func TestRequestSelection(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name     string    `json:"name"`
		Target   identity  `json:"target"`
		Requests []request `json:"requests"`
		Want     string    `json:"want"`
	}](t, "selection")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			equal(t, latest(t, tc.Target, tc.Requests), tc.Want)
			slices.Reverse(tc.Requests)
			equal(t, latest(t, tc.Target, tc.Requests), tc.Want)
		})
	}
}
