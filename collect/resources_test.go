package collect_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func request() collect.Request {
	return collect.Request{
		Scope: evidence.Scope{
			Origin: "origin-1", Group: "openbao.org", Version: "v1alpha1", Resource: "openbaoclusters",
			Namespace: "team", Action: evidence.ReadList, Selector: "app.kubernetes.io/managed-by=openbao-operator",
		},
		Contract: adapter.V1Alpha1Snapshot, Limits: collect.DefaultLimits(), Projection: adapter.DefaultProjectionLimits(),
	}
}

func object(uid string) string {
	return fmt.Sprintf(`{"metadata":{"namespace":"team","name":%q,"uid":%q},`+
		`"spec":{"credentials":"SPEC_CANARY"},"status":{"message":"MESSAGE_CANARY"}}`, uid, uid)
}

func list(next, version string, items ...string) string {
	return fmt.Sprintf(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoClusterList",`+
		`"metadata":{"continue":%q,"resourceVersion":%q},"items":[%s]}`, next, version, strings.Join(items, ","))
}

func testClient(t *testing.T, handler http.HandlerFunc) *collect.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := collect.NewClient(server.URL+"/prefix", http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeBody(t *testing.T, writer http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(writer, body); err != nil {
		t.Error(err)
	}
}

func TestPaginationScopeAndSafeEvidence(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.Method != http.MethodGet || incoming.URL.Path != "/prefix/apis/openbao.org/v1alpha1/namespaces/team/openbaoclusters" ||
			incoming.Header.Get("Accept") != "application/json" {
			t.Error("request expanded or changed the declared scope")
		}
		if incoming.URL.Query().Get("labelSelector") != request().Scope.Selector || incoming.URL.Query().Get("limit") != "100" {
			t.Error("selector or page limit changed")
		}
		if calls.Add(1) == 1 {
			writeBody(t, writer, list("NEXT_CANARY+/=", "10", object("b")))
			return
		}
		if incoming.URL.Query().Get("continue") != "NEXT_CANARY+/=" || len(incoming.URL.Query()) != 3 {
			t.Error("opaque continuation was changed or extra query parameters were added")
		}
		writeBody(t, writer, list("", "10", object("a")))
	})
	result, err := client.Clusters(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if result.Collection.Validate() != nil || result.Collection.Outcome != evidence.CollectionComplete ||
		result.Requests != 2 || result.Collection.Items != 2 || len(result.Observations) != 2 {
		t.Fatalf("incomplete result: %+v", result.Collection)
	}
	if result.Observations[0].Reference.Identity.UID != "a" || result.Observations[1].Reference.Identity.UID != "b" {
		t.Fatal("observations did not use deterministic identity order")
	}
	for _, value := range result.Observations {
		read := *value.Reference.CollectedAt.Value
		if read.Before(result.Collection.Interval.StartedAt) || read.After(result.Collection.Interval.CompletedAt) {
			t.Fatal("read time fell outside the collection interval")
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "CANARY") {
		t.Fatal("raw resource, continuation, or provider text escaped projection")
	}
	var replay collect.Result[evidence.Cluster]
	if err := json.Unmarshal(encoded, &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, replay) {
		t.Fatal("serialized evidence changed on replay")
	}
}

func TestIncompletePagesPreservePriorFacts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		body    string
		status  int
		outcome evidence.CollectionOutcome
		count   int
	}{
		{"denied", "ERROR_CANARY", http.StatusForbidden, evidence.CollectionDenied, 1},
		{"unsupported", "ERROR_CANARY", http.StatusNotFound, evidence.CollectionUnsupported, 1},
		{"failed", "ERROR_CANARY", http.StatusInternalServerError, evidence.CollectionFailed, 1},
		{"expired", "ERROR_CANARY", http.StatusGone, evidence.CollectionFailed, 1},
		{"duplicate-uid", list("", "10", object("b")), http.StatusOK, evidence.CollectionInvalid, 1},
		{"atomic-page", list("", "10", object("a"), object("b")), http.StatusOK, evidence.CollectionInvalid, 1},
		{"changed-version", list("", "11", object("a")), http.StatusOK, evidence.CollectionInvalid, 1},
		{"repeated-token", list("next", "10", object("a")), http.StatusOK, evidence.CollectionPaginationLoop, 2},
		{"invalid-page", `{"items":[],"items":[]}`, http.StatusOK, evidence.CollectionInvalid, 1},
		{
			"wrong-scope", list("", "10", `{"metadata":{"namespace":"other","name":"a","uid":"a"}}`),
			http.StatusOK, evidence.CollectionInvalid, 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := testClient(t, func(writer http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					writeBody(t, writer, list("next", "10", object("b")))
					return
				}
				writer.WriteHeader(tc.status)
				writeBody(t, writer, tc.body)
			})
			result, err := client.Clusters(context.Background(), request())
			if err != nil {
				t.Fatal(err)
			}
			if result.Collection.Validate() != nil || result.Collection.Outcome != tc.outcome ||
				len(result.Observations) != tc.count || result.Collection.AbsenceConfirmed() || result.Requests != 2 {
				t.Fatalf("outcome=%s records=%d requests=%d", result.Collection.Outcome, len(result.Observations), result.Requests)
			}
		})
	}
}

func TestCollectionLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		change  func(*collect.Request)
		outcome evidence.CollectionOutcome
		count   int
		reads   int
	}{
		{"pages", func(r *collect.Request) { r.Limits.MaxPages = 1 }, evidence.CollectionPageLimit, 1, 1},
		{
			"candidates", func(r *collect.Request) { r.Limits.MaxCandidates = 1; r.Limits.PageSize = 1 },
			evidence.CollectionSizeLimit, 1, 2,
		},
		{
			"bytes", func(r *collect.Request) { r.Limits.MaxBytes = len(list("next", "10", object("b"))) },
			evidence.CollectionSizeLimit, 1, 1,
		},
		{"response", func(r *collect.Request) { r.Limits.MaxResponseBytes = 2 }, evidence.CollectionSizeLimit, 0, 1},
		{"retained", func(r *collect.Request) { r.Limits.MaxRetainedBytes = 1 }, evidence.CollectionSizeLimit, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := testClient(t, func(writer http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					writeBody(t, writer, list("next", "10", object("b")))
				} else {
					writeBody(t, writer, list("", "10", object("a")))
				}
			})
			req := request()
			tc.change(&req)
			result, err := client.Clusters(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if result.Collection.Outcome != tc.outcome || len(result.Observations) != tc.count || result.Requests != tc.reads {
				t.Fatalf("outcome=%s records=%d requests=%d", result.Collection.Outcome, len(result.Observations), result.Requests)
			}
		})
	}
}

func TestNamedGetPinsUIDAndDoesNotReadRelatedResources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, uid string
		status    int
		want      evidence.CollectionOutcome
	}{
		{"current", "cluster-1", http.StatusOK, evidence.CollectionComplete},
		{"replacement", "cluster-2", http.StatusOK, evidence.CollectionReplaced},
		{"deletion", "", http.StatusNotFound, evidence.CollectionDeleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
				if incoming.URL.Path != "/prefix/apis/openbao.org/v1alpha1/namespaces/team/openbaoclusters/bao" ||
					incoming.URL.RawQuery != "" {
					t.Error("named read changed its resource boundary")
				}
				writer.WriteHeader(tc.status)
				writeBody(t, writer, fmt.Sprintf(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",`+
					`"metadata":{"namespace":"team","name":"bao","uid":%q}}`, tc.uid))
			})
			req := request()
			req.Scope.Action, req.Scope.Name, req.Scope.Selector, req.ExpectedUID = evidence.ReadGet, "bao", "", "cluster-1"
			result, err := client.Clusters(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if result.Collection.Outcome != tc.want || result.Requests != 1 || result.Collection.AbsenceConfirmed() {
				t.Fatalf("named read outcome = %s", result.Collection.Outcome)
			}
			if tc.want != evidence.CollectionComplete && len(result.Observations) != 0 {
				t.Fatal("replacement or deleted resource was returned as the selected target")
			}
		})
	}
}

func TestTimeoutCancellationAndRedirects(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(_ http.ResponseWriter, incoming *http.Request) { <-incoming.Context().Done() })
	req := request()
	req.Limits.Timeout = 10 * time.Millisecond
	result, err := client.Clusters(context.Background(), req)
	if err != nil || result.Collection.Outcome != evidence.CollectionTimeout {
		t.Fatalf("timeout: outcome=%s error=%v", result.Collection.Outcome, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = client.Clusters(ctx, request())
	if err != nil || result.Collection.Outcome != evidence.CollectionCancelled || result.Requests != 0 {
		t.Fatal("pre-cancelled read made a request or lost cancellation")
	}
	var calls atomic.Int32
	redirecting := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
		calls.Add(1)
		http.Redirect(writer, incoming, "/forbidden-redirect", http.StatusTemporaryRedirect)
	})
	result, err = redirecting.Clusters(context.Background(), request())
	if err != nil || result.Collection.Outcome != evidence.CollectionFailed || calls.Load() != 1 {
		t.Fatal("redirect broadened the read or was reported complete")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInvalidRequestsNeverReachTransport(t *testing.T) {
	t.Parallel()
	client, err := collect.NewClient("https://example.test", roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid request reached transport")
		return nil, errors.New("ERROR_CANARY")
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*collect.Request){
		func(r *collect.Request) { r.Limits.MaxPages = 0 },
		func(r *collect.Request) { r.Limits.PageSize = 0 },
		func(r *collect.Request) { r.Scope.Namespace = "" },
		func(r *collect.Request) { r.Scope.Namespace = "../other" },
		func(r *collect.Request) { r.Scope.Resource = "pods" },
		func(r *collect.Request) { r.Scope.Action = evidence.ReadDiscovery },
		func(r *collect.Request) { r.Contract = adapter.KubernetesCoreV1 },
		func(r *collect.Request) { r.ExpectedUID = "cluster-1" },
	} {
		req := request()
		change(&req)
		_, err := client.Clusters(context.Background(), req)
		if !errors.Is(err, collect.ErrInvalidRequest) {
			t.Fatal("invalid request did not return the bounded caller error")
		}
	}
}

func TestEmptyListsRequireCompleteValidReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		status     int
		absent     bool
	}{
		{"valid", list("", "10"), http.StatusOK, true},
		{"denied", "ERROR_CANARY", http.StatusForbidden, false},
		{"unsupported", "ERROR_CANARY", http.StatusNotFound, false},
		{"malformed", `{"items":[]}`, http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				writeBody(t, writer, tc.body)
			})
			result, err := client.Clusters(context.Background(), request())
			if err != nil || result.Collection.AbsenceConfirmed() != tc.absent || result.Collection.Validate() != nil {
				t.Fatalf("absence=%v error=%v", result.Collection.AbsenceConfirmed(), err)
			}
		})
	}
}

func TestCancellationPreservesAnAdmittedPage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client, err := collect.NewClient("https://example.test", roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 2 {
			cancel()
			return nil, errors.New("ERROR_CANARY")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(list("next", "10", object("b"))))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Clusters(ctx, request())
	if err != nil || result.Collection.Outcome != evidence.CollectionCancelled || len(result.Observations) != 1 {
		t.Fatal("cancellation discarded admitted facts or became a complete collection")
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "CANARY") {
		t.Fatal("raw client error escaped the outcome boundary")
	}
}

func TestApprovedResourcePathsAndExplicitAllNamespaces(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind, group, version, resource string
		contract                       adapter.Contract
		read                           func(*collect.Client, collect.Request) (evidence.Collection, error)
	}{
		{
			"Pod", "", "v1", "pods", adapter.KubernetesCoreV1,
			func(c *collect.Client, r collect.Request) (evidence.Collection, error) {
				result, err := c.Pods(context.Background(), r)
				return result.Collection, err
			},
		},
		{
			"StatefulSet", "apps", "v1", "statefulsets", adapter.KubernetesAppsV1,
			func(c *collect.Client, r collect.Request) (evidence.Collection, error) {
				result, err := c.StatefulSets(context.Background(), r)
				return result.Collection, err
			},
		},
		{
			"OpenBaoRestore", "openbao.org", "v1alpha1", "openbaorestores", adapter.V1Alpha1Snapshot,
			func(c *collect.Client, r collect.Request) (evidence.Collection, error) {
				result, err := c.Restores(context.Background(), r)
				return result.Collection, err
			},
		},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			path := "/prefix/apis/" + tc.group + "/" + tc.version + "/" + tc.resource
			if tc.group == "" {
				path = "/prefix/api/" + tc.version + "/" + tc.resource
			}
			client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
				if incoming.URL.Path != path {
					t.Errorf("path=%s, want %s", incoming.URL.Path, path)
				}
				apiVersion := tc.version
				if tc.group != "" {
					apiVersion = tc.group + "/" + tc.version
				}
				writeBody(t, writer, fmt.Sprintf(`{"apiVersion":%q,"kind":%q,`+
					`"metadata":{"resourceVersion":"10"},"items":[%s]}`, apiVersion, tc.kind+"List", object("a")))
			})
			req := request()
			req.Scope.Group, req.Scope.Version, req.Scope.Resource = tc.group, tc.version, tc.resource
			req.Scope.Namespace, req.AllowAllNamespaces, req.Contract = "", true, tc.contract
			collection, err := tc.read(client, req)
			if err != nil || collection.Outcome != evidence.CollectionComplete || collection.Items != 1 {
				t.Fatalf("outcome=%s error=%v", collection.Outcome, err)
			}
		})
	}
}

func TestEndpointValidationDoesNotExposeCredentials(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{
		"https://user:SECRET_CANARY@example.test", "https://example.test?token=SECRET_CANARY",
		"https://example.test#SECRET_CANARY", "file:///SECRET_CANARY", "https://example.test/../other",
		"https://example.test/%2e%2e/other",
	} {
		_, err := collect.NewClient(endpoint, http.DefaultTransport)
		if !errors.Is(err, collect.ErrInvalidRequest) || strings.Contains(err.Error(), "CANARY") {
			t.Fatal("invalid endpoint was accepted or exposed its input")
		}
	}
	if _, err := collect.NewClient("https://example.test", nil); !errors.Is(err, collect.ErrInvalidRequest) {
		t.Fatal("missing caller transport was defaulted")
	}
}

type countedBody struct {
	io.Reader
	read   int
	closed bool
}

func (b *countedBody) Read(data []byte) (int, error) {
	count, err := b.Reader.Read(data)
	b.read += count
	return count, err
}

func (b *countedBody) Close() error { b.closed = true; return nil }

func TestResponseBytesAreBoundedBeforeJSONDecoding(t *testing.T) {
	t.Parallel()
	body := &countedBody{Reader: strings.NewReader(strings.Repeat("UNBOUNDED_CANARY", 1000))}
	client, err := collect.NewClient("https://example.test", roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	req := request()
	req.Limits.MaxResponseBytes = 16
	result, err := client.Clusters(context.Background(), req)
	if err != nil || result.Collection.Outcome != evidence.CollectionSizeLimit || body.read != 17 || !body.closed {
		t.Fatal("response limit did not stop and close the body before projection")
	}
}

func TestTransportErrorIsClassifiedWithoutProviderText(t *testing.T) {
	t.Parallel()
	client, err := collect.NewClient("https://example.test", roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("token=ERROR_CANARY")
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Clusters(context.Background(), request())
	if err != nil || result.Collection.Outcome != evidence.CollectionFailed || result.Requests != 1 {
		t.Fatal("unclassified transport failure was returned raw or treated as complete")
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "CANARY") {
		t.Fatal("provider error escaped serialization")
	}
}
