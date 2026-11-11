package adapter_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
)

func TestListEnvelopeProjection(t *testing.T) {
	t.Parallel()
	data := `{"apiVersion":"v1","kind":"PodList","metadata":{"resourceVersion":"10","continue":"TOKEN_CANARY"},` +
		`"items":[{"metadata":{"namespace":"team","name":"bao-0","uid":"pod-1"},"spec":{"token":"SPEC_CANARY"}}]}`
	page, err := adapter.ProjectPodList([]byte(data), workloadOptions(adapter.KubernetesCoreV1))
	if err != nil {
		t.Fatal(err)
	}
	if page.Continue != "TOKEN_CANARY" || page.ResourceVersion != "10" || len(page.Observations) != 1 ||
		page.Observations[0].Metadata.Reference.APIVersion != "v1" || page.Observations[0].Validate() != nil {
		t.Fatal("list envelope facts or inherited item type were lost")
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "CANARY") {
		t.Fatal("list control token or unapproved resource fields escaped serialization")
	}
}

func TestMalformedListEnvelopes(t *testing.T) {
	t.Parallel()
	valid := `{"apiVersion":"v1","kind":"PodList","metadata":{"resourceVersion":"10"},"items":[]}`
	metadata := `"metadata":{"namespace":"team","name":"bao-0","uid":"pod-1"}`
	for _, data := range []string{
		strings.Replace(valid, `"resourceVersion":"10"`, `"resourceVersion":null`, 1),
		strings.Replace(valid, `"resourceVersion":"10"`, `"continue":"TOKEN_CANARY"`, 1),
		strings.Replace(valid, `"resourceVersion":"10"`, `"resourceVersion":"10","continue":null`, 1),
		strings.Replace(valid, `"PodList"`, `"Pod"`, 1),
		strings.Replace(valid, `"items":[]`, `"items":null`, 1),
		strings.Replace(valid, `"items":[]`, `"items":[],"items":[]`, 1),
		strings.Replace(valid, `"items":[]`, `"items":[{"apiVersion":null,`+metadata+`}]`, 1),
		strings.Replace(valid, `"items":[]`, `"items":[{"apiVersion":"",`+metadata+`}]`, 1),
		strings.Replace(valid, `"items":[]`, `"items":[{"apiVersion":"apps/v1",`+metadata+`}]`, 1),
		strings.Replace(valid, `"items":[]`, `"items":[{"kind":null,`+metadata+`}]`, 1),
		strings.Replace(valid, `"items":[]`, `"items":[{"kind":"",`+metadata+`}]`, 1),
	} {
		if _, err := adapter.ProjectPodList([]byte(data), workloadOptions(adapter.KubernetesCoreV1)); err == nil {
			t.Fatal("malformed list was admitted")
		}
	}
	for _, options := range []adapter.ProjectionOptions{
		{Contract: adapter.KubernetesCoreV1, Origin: "origin", Limits: adapter.DefaultProjectionLimits()},
		{Contract: adapter.KubernetesCoreV1, CollectedAt: projectionOptions().CollectedAt, Limits: adapter.DefaultProjectionLimits()},
	} {
		if _, err := adapter.ProjectPodList([]byte(valid), options); err == nil {
			t.Fatal("empty list bypassed projection context validation")
		}
	}
}

func FuzzProjectList(f *testing.F) {
	f.Add([]byte(`{"apiVersion":"v1","kind":"PodList","metadata":{"resourceVersion":"10"},"items":[]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		page, err := adapter.ProjectPodList(data, workloadOptions(adapter.KubernetesCoreV1))
		if err != nil {
			return
		}
		for _, value := range page.Observations {
			if value.Validate() != nil {
				t.Fatal("list retained invalid evidence")
			}
		}
		if _, err := json.Marshal(page); err != nil {
			t.Fatal(err)
		}
	})
}
