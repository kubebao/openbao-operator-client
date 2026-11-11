package contracttest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

type workloadTransport struct {
	t           *testing.T
	denied      bool
	replaced    bool
	restoreMode bool
	reads       int
}

func (r *workloadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.t.Helper()
	r.reads++
	if req.Method != http.MethodGet {
		r.t.Fatal("diagnostic collection attempted a mutation")
	}
	body, status := "", http.StatusOK
	switch {
	case strings.HasSuffix(req.URL.Path, "/statefulsets"), strings.HasSuffix(req.URL.Path, "/pods"):
		name, api, kind := "statefulset", "apps/v1", "StatefulSetList"
		if strings.HasSuffix(req.URL.Path, "/pods") {
			name, api, kind = "pod", "v1", "PodList"
			if r.denied {
				status = http.StatusForbidden
			}
		}
		raw, err := os.ReadFile("../../adapter/testdata/" + name + ".json")
		if err != nil {
			r.t.Fatal(err)
		}
		body = fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"metadata":{"resourceVersion":"10"},"items":[%s]}`, api, kind, raw)
	case strings.HasSuffix(req.URL.Path, "/openbaorestores"):
		raw, err := os.ReadFile("../../adapter/testdata/restore.json")
		if err != nil {
			r.t.Fatal(err)
		}
		body = fmt.Sprintf(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoRestoreList",`+
			`"metadata":{"resourceVersion":"10"},"items":[%s]}`, raw)
		if r.denied {
			status = http.StatusForbidden
		}
	case strings.HasSuffix(req.URL.Path, "/openbaoclusters/bao"):
		uid := "cluster-1"
		if r.replaced && r.reads > 1 {
			uid = "replacement"
		}
		hold := ""
		if r.restoreMode {
			hold = `,"annotations":{"openbao.org/restore-hold":"operation-1"}`
		}
		body = fmt.Sprintf(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",`+
			`"metadata":{"namespace":"team","name":"bao","uid":%q,"generation":4%s},`+
			`"spec":{"replicas":3},"status":{"observedGeneration":4}}`, uid, hold)
	default:
		r.t.Fatal("diagnosis expanded the declared resource scope")
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestCollectedWorkloadsRetainDiagnosticReadBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		denied, moved bool
		findings      int
	}{
		{"complete", false, false, 3},
		{"denied-pods", true, false, 1},
		{"replaced-target", false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &workloadTransport{t: t, denied: tc.denied, replaced: tc.moved}
			client, err := collect.NewClient("https://api.invalid", transport)
			if err != nil {
				t.Fatal(err)
			}
			request := collect.Request{
				Scope: evidence.Scope{
					Origin: "cluster-a", Group: "openbao.org", Version: "v1alpha1", Resource: "openbaoclusters",
					Namespace: "team", Name: "bao", Action: evidence.ReadGet,
				},
				Contract: adapter.V1Alpha1Snapshot, Limits: collect.DefaultLimits(), Projection: adapter.DefaultProjectionLimits(),
			}
			initial, err := client.Clusters(context.Background(), request)
			if err != nil || len(initial.Observations) != 1 {
				t.Fatal("initial synthetic cluster read failed")
			}
			cluster := initial.Observations[0]
			request.ExpectedUID = cluster.Reference.Identity.UID
			sets, pods := request, request
			sets.Contract, sets.ExpectedUID = adapter.KubernetesAppsV1, ""
			sets.Scope.Group, sets.Scope.Version, sets.Scope.Resource, sets.Scope.Name, sets.Scope.Action =
				"apps", "v1", "statefulsets", "", evidence.ReadList
			pods.Contract, pods.ExpectedUID = adapter.KubernetesCoreV1, ""
			pods.Scope.Group, pods.Scope.Version, pods.Scope.Resource, pods.Scope.Name, pods.Scope.Action =
				"", "v1", "pods", "", evidence.ReadList
			related, err := client.Related(context.Background(), cluster, collect.RelatedPlan{
				StatefulSets: &sets, Pods: &pods, Verify: request, Limits: collect.DefaultLimits(),
			})
			if err != nil {
				t.Fatal(err)
			}
			input := diagnose.ClusterInput{
				Subject: cluster.Reference.Identity, AdapterContract: cluster.AdapterContract, Observation: &cluster,
				Collection: initial.Collection, Verification: &related.Verification,
				StatefulSets: &diagnose.ResourceInput[evidence.StatefulSet]{
					AdapterContract: string(sets.Contract),
					Collection:      related.StatefulSets.Collection, Observations: related.StatefulSets.Observations,
				},
				Pods: &diagnose.ResourceInput[evidence.Pod]{
					AdapterContract: string(pods.Contract),
					Collection:      related.Pods.Collection, Observations: related.Pods.Observations,
				},
			}
			policy := diagnose.DefaultPolicy(time.Now(), []string{cluster.AdapterContract, string(sets.Contract), string(pods.Contract)})
			policy.Rules = []diagnose.RuleID{diagnose.RuleWorkload, diagnose.RulePod}
			report, err := diagnose.Analyze(
				diagnose.Capture{SchemaVersion: evidence.SchemaVersion, Clusters: []diagnose.ClusterInput{input}},
				policy,
			)
			if err != nil || len(report.Findings) != tc.findings || transport.reads != 4 {
				t.Fatalf("collected workload diagnosis: findings %d reads %d error %v", len(report.Findings), transport.reads, err)
			}
			for _, entry := range report.Coverage {
				if (tc.denied && entry.Rule == diagnose.RulePod || tc.moved &&
					(entry.Rule == diagnose.RulePod || entry.Rule == diagnose.RuleWorkload)) && entry.State != diagnose.Insufficient {
					t.Fatal("incomplete collection became complete diagnostic coverage")
				}
			}
		})
	}
}
