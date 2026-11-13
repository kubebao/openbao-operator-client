package main

import (
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func main() {
	reference := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	raw := []byte(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",` +
		`"metadata":{"namespace":"team","name":"bao","uid":"cluster-1","generation":4,"creationTimestamp":"2026-11-10T09:00:00Z"},` +
		`"spec":{"replicas":3,"paused":false,"backup":{}},` +
		`"status":{"observedGeneration":4,"readyReplicas":0,"backup":{"consecutiveFailures":0,"lastBackupTime":"2026-11-09T09:00:00Z"}}}`)
	cluster, err := adapter.ProjectCluster(raw, adapter.ProjectionOptions{
		Origin: "cluster-a", Contract: adapter.OpenBaoV1Alpha1,
		CollectedAt: reference, Limits: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		panic(err)
	}
	input := diagnose.ClusterInput{
		Subject: cluster.Reference.Identity, Observation: &cluster,
		Collection: evidence.Collection{
			Scope: evidence.Scope{
				Origin: "cluster-a", Group: "openbao.org", Version: "v1alpha1",
				Resource: "openbaoclusters", Namespace: "team", Name: "bao", Action: evidence.ReadGet,
			},
			Outcome: evidence.CollectionComplete, Items: 1, Interval: evidence.Interval{StartedAt: reference, CompletedAt: reference},
		},
	}
	report, err := diagnose.Analyze(diagnose.Capture{Clusters: []diagnose.ClusterInput{input}},
		diagnose.DefaultPolicy(reference))
	if err != nil {
		panic(err)
	}
	fmt.Printf("%d findings, %d coverage entries\n", len(report.Findings), len(report.Coverage))
}
