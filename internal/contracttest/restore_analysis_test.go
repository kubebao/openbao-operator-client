package contracttest

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestCollectedRestoresRetainUIDAndReadBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		denied, moved bool
		findings      int
	}{
		{"complete", false, false, 1}, {"denied", true, false, 0}, {"replaced", false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &workloadTransport{t: t, denied: tc.denied, replaced: tc.moved, restoreMode: true}
			client, err := collect.NewClient("https://api.invalid", transport)
			if err != nil {
				t.Fatal(err)
			}
			request := collect.Request{
				Scope: evidence.Scope{
					Origin: "cluster-a", Group: "openbao.org", Version: "v1alpha1", Resource: "openbaoclusters",
					Namespace: "team", Name: "bao", Action: evidence.ReadGet,
				},
				Contract: adapter.OpenBaoV1Alpha1, Limits: collect.DefaultLimits(), Projection: adapter.DefaultProjectionLimits(),
			}
			initial, err := client.Clusters(context.Background(), request)
			if err != nil || len(initial.Observations) != 1 {
				t.Fatal("initial synthetic cluster read failed")
			}
			cluster := initial.Observations[0]
			request.ExpectedUID = cluster.Reference.Identity.UID
			restores := request
			restores.Scope.Resource, restores.Scope.Name = "openbaorestores", ""
			restores.Scope.Action, restores.ExpectedUID = evidence.ReadList, ""
			related, err := client.Related(context.Background(), cluster, collect.RelatedPlan{
				Restores: &restores, Verify: request, Limits: collect.DefaultLimits(),
			})
			if err != nil {
				t.Fatal(err)
			}
			input := diagnose.ClusterInput{
				Subject: cluster.Reference.Identity, Observation: &cluster,
				Collection: initial.Collection, Verification: &related.Verification,
				Restores: &diagnose.ResourceInput[evidence.Restore]{
					Collection: related.Restores.Collection, Observations: related.Restores.Observations,
				},
			}
			info, err := adapter.Describe(request.Contract)
			if err != nil || !slices.Contains(info.Capabilities, adapter.CapabilityRestoreUIDBinding) {
				t.Fatal("fixture contract has no inspected restore binding capability")
			}
			policy := diagnose.DefaultPolicy(time.Now())
			policy.Rules = []diagnose.RuleID{diagnose.RuleRestore}
			report, err := diagnose.Analyze(
				diagnose.Capture{Clusters: []diagnose.ClusterInput{input}},
				policy,
			)
			if err != nil || len(report.Findings) != tc.findings || transport.reads != 3 {
				t.Fatalf("collected restore diagnosis: findings %d reads %d error %v", len(report.Findings), transport.reads, err)
			}
			if tc.findings > 0 && report.Findings[0].Severity != diagnose.Error {
				t.Fatal("held restore concern lost its severity")
			}
			encoded, err := json.Marshal(report)
			if err != nil || strings.Contains(string(encoded), "CANARY") {
				t.Fatal("source/provider text escaped collection into the report")
			}
		})
	}
}
