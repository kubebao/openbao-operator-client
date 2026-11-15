package collect_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// fixedTransport answers every request with one OpenBaoCluster.
type fixedTransport string

func (body fixedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(strings.NewReader(string(body))),
		Request:       request,
	}, nil
}

func ExampleClient_Clusters() {
	cluster := fixedTransport(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",` +
		`"metadata":{"namespace":"team","name":"bao","uid":"cluster-1","generation":4},` +
		`"spec":{"replicas":3},"status":{"observedGeneration":4,"readyReplicas":3}}`)
	client, err := collect.NewClient("https://api.example.test", cluster)
	if err != nil {
		fmt.Println(err)
		return
	}
	result, err := client.Clusters(context.Background(), collect.Request{
		Scope: evidence.Scope{
			Origin:    "cluster-a",
			Group:     "openbao.org",
			Version:   "v1alpha1",
			Resource:  "openbaoclusters",
			Namespace: "team",
			Name:      "bao",
			Action:    evidence.ReadGet,
		},
		Contract:   adapter.OpenBaoV1Alpha1,
		Limits:     collect.DefaultLimits(),
		Projection: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(result.Collection.Outcome, len(result.Observations), result.Requests)
	// Output: complete 1 1
}
