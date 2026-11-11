package adapter_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func discoveryDocument(resources string) []byte {
	return []byte(`{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"openbao.org/v1alpha1","resources":[` + resources + `]}`)
}

func TestDiscoveryRetainsFactsWithoutQualifyingCapabilities(t *testing.T) {
	t.Parallel()
	raw := discoveryDocument(`{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true,` +
		`"verbs":["watch","create","list","get"],"shortNames":["SECRET_CANARY"],"message":"PROVIDER_CANARY"}`)
	value, err := adapter.ProjectDiscovery(raw, "openbaoclusters", projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if value.Validate() != nil || !adapter.Advertises(value, evidence.VerbGet) || !adapter.Advertises(value, evidence.VerbWatch) ||
		len(value.Verbs.Values) != 3 || value.Verbs.Values[0] != evidence.VerbGet {
		t.Fatal("advertised read verbs were lost or not canonical")
	}
	encoded, err := json.Marshal(value)
	if err != nil || strings.Contains(string(encoded), "CANARY") || strings.Contains(string(encoded), "create") {
		t.Fatal("unapproved discovery fields or mutation verbs escaped projection")
	}
	var replay evidence.Discovery
	if err := json.Unmarshal(encoded, &replay); err != nil || replay.Validate() != nil {
		t.Fatal("discovery did not survive serialized replay")
	}
	value.Scope.Group = "other.example"
	if adapter.Advertises(value, evidence.VerbGet) {
		t.Fatal("contract was applied to discovery from another group")
	}
}

func TestDiscoveryAbsenceAndMalformedVerbsStayDistinct(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, fields string
		state        evidence.ObjectState
		verbs        evidence.ValueState
	}{
		{"absent", ``, evidence.ObjectAbsent, evidence.ValueUnknown},
		{
			"missing-verbs", `{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true}`,
			evidence.ObjectReported, evidence.ValueUnknown,
		},
		{
			"null-verbs", `{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true,"verbs":null}`,
			evidence.ObjectReported, evidence.ValueInvalid,
		},
		{
			"duplicate-verbs", `{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true,"verbs":["get","get"]}`,
			evidence.ObjectReported, evidence.ValueInvalid,
		},
		{
			"cluster-scoped", `{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":false,"verbs":["get"]}`,
			evidence.ObjectReported, evidence.ValueReported,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := adapter.ProjectDiscovery(discoveryDocument(tc.fields), "openbaoclusters", projectionOptions())
			if err != nil || value.Validate() != nil || value.State != tc.state || value.Verbs.State != tc.verbs ||
				adapter.Advertises(value, evidence.VerbGet) {
				t.Fatalf("discovery state=%s verbs=%s error=%v", value.State, value.Verbs.State, err)
			}
		})
	}
	duplicate := `{"name":"openbaoclusters"},{"name":"openbaoclusters"}`
	_, err := adapter.ProjectDiscovery(discoveryDocument(duplicate), "openbaoclusters", projectionOptions())
	assertProjectionError(t, err, adapter.InvalidInput)
	wrong := []byte(strings.Replace(string(discoveryDocument("")), "openbao.org/v1alpha1", "other.example/v1", 1))
	_, err = adapter.ProjectDiscovery(wrong, "openbaoclusters", projectionOptions())
	assertProjectionError(t, err, adapter.ResourceMismatch)
}

func FuzzProjectDiscovery(f *testing.F) {
	f.Add(discoveryDocument(`{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true,"verbs":["get"]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := adapter.ProjectDiscovery(data, "openbaoclusters", projectionOptions())
		if err == nil && value.Validate() != nil {
			t.Fatal("invalid normalized discovery")
		}
	})
}
