package collect_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func targetDocument(uid, generation string) string {
	return fmt.Sprintf(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",`+
		`"metadata":{"namespace":"team","name":"bao","uid":%q%s}}`, uid, generation)
}

func selectedTarget(t *testing.T) evidence.Cluster {
	t.Helper()
	value, err := adapter.ProjectCluster([]byte(targetDocument("cluster-1", `,"generation":1`)), adapter.ProjectionOptions{
		Origin: "origin-1", Contract: adapter.V1Alpha1Snapshot, CollectedAt: time.Now().UTC().Add(-time.Second),
		Limits: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func verifyRequest() collect.Request {
	r := request()
	r.Scope.Action, r.Scope.Name, r.Scope.Selector, r.ExpectedUID = evidence.ReadGet, "bao", "", "cluster-1"
	return r
}

func TestFinalTargetVerification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, uid, generation string
		status                int
		state                 evidence.VerificationState
	}{
		{"same", "cluster-1", `,"generation":1`, http.StatusOK, evidence.VerificationVerified},
		{"changed", "cluster-1", `,"generation":2`, http.StatusOK, evidence.VerificationChanged},
		{"missing-generation", "cluster-1", ``, http.StatusOK, evidence.VerificationInsufficient},
		{"zero-generation", "cluster-1", `,"generation":0`, http.StatusOK, evidence.VerificationInsufficient},
		{"replacement", "cluster-2", `,"generation":1`, http.StatusOK, evidence.VerificationReplaced},
		{"deleted", "", ``, http.StatusNotFound, evidence.VerificationDeleted},
		{"denied", "", ``, http.StatusForbidden, evidence.VerificationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
				if !strings.HasSuffix(incoming.URL.Path, "/namespaces/team/openbaoclusters/bao") || incoming.URL.RawQuery != "" {
					t.Error("verification changed target or performed discovery")
				}
				writer.WriteHeader(tc.status)
				writeBody(t, writer, targetDocument(tc.uid, tc.generation))
			})
			result, err := client.VerifyTarget(context.Background(), selectedTarget(t), verifyRequest())
			if err != nil || result.Requests != 1 || result.Verification.State != tc.state || result.Verification.Validate() != nil {
				t.Fatalf("state=%s error=%v validation=%v", result.Verification.State, err, result.Verification.Validate())
			}
			encoded, err := json.Marshal(result.Verification)
			if err != nil {
				t.Fatal(err)
			}
			var replay evidence.TargetVerification
			if err := json.Unmarshal(encoded, &replay); err != nil || replay.Validate() != nil {
				t.Fatal("verification did not survive replay")
			}
			if tc.state == evidence.VerificationReplaced && result.Verification.After != nil {
				t.Fatal("replacement was returned as the selected identity")
			}
			if replay.After != nil {
				replay.After.Identity.UID = "different"
				if replay.Validate() == nil {
					t.Fatal("forged verified identity accepted")
				}
			}
		})
	}
}

func TestDiscoveryReadsOnlyItsDeclaredEndpoint(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.URL.Path != "/prefix/apis/openbao.org/v1alpha1" || incoming.URL.RawQuery != "" {
			t.Error("discovery broadened to a resource or another endpoint")
		}
		writeBody(t, writer, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"openbao.org/v1alpha1",`+
			`"resources":[{"name":"openbaoclusters","kind":"OpenBaoCluster","namespaced":true,"verbs":["get","list"]}]}`)
	})
	r := request()
	r.Scope.Action, r.Scope.Namespace, r.Scope.Selector = evidence.ReadDiscovery, "", ""
	result, err := client.Discover(context.Background(), r)
	if err != nil || result.Requests != 1 || result.Collection.Validate() != nil ||
		result.Collection.Outcome != evidence.CollectionComplete || result.Collection.AbsenceConfirmed() ||
		len(result.Observations) != 1 || !adapter.Advertises(result.Observations[0], evidence.VerbList) {
		t.Fatalf("discovery outcome=%s error=%v", result.Collection.Outcome, err)
	}
}
