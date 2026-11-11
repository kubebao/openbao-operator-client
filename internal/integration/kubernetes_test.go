// Package integration qualifies selected Kubernetes fields using a caller-supplied
// disposable test cluster. It is not imported by the client library.
package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func kubectl(t *testing.T, arguments ...string) []byte {
	t.Helper()
	kubeconfig := os.Getenv("OPENBAO_CLIENT_TEST_KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("set OPENBAO_CLIENT_TEST_KUBECONFIG to the disposable client-k134 cluster")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args := append([]string{"--kubeconfig", kubeconfig, "--context", "kind-client-k134", "-n", "client-k134"}, arguments...)
	//nolint:gosec // G204: fixed kubectl and namespace; the caller supplies only a disposable kubeconfig.
	output, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		t.Fatal("read the scoped Kubernetes test resource; provider output is excluded")
	}
	return output
}

func options() adapter.ProjectionOptions {
	return adapter.ProjectionOptions{
		Contract: adapter.KubernetesCoreV1, Origin: "kind-client-k134", CollectedAt: time.Now().UTC(),
		Limits: adapter.DefaultProjectionLimits(),
	}
}

func TestKubernetes134PodConditionAcknowledgement(t *testing.T) {
	var version struct {
		Server struct {
			Version string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(
		kubectl(t, "version", "-o", "json"),
		&version,
	); err != nil ||
		!strings.HasPrefix(version.Server.Version, "v1.34.") {
		t.Fatal("this qualification requires Kubernetes 1.34")
	}
	for _, value := range []struct {
		name   string
		status evidence.ConditionStatus
	}{{"ready-true", evidence.ConditionTrue}, {"ready-false", evidence.ConditionFalse}} {
		pod, err := adapter.ProjectPod(kubectl(t, "get", "pod", value.name, "-o", "json"), options())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, condition := range pod.Conditions.Values {
			if condition.Type != "Ready" {
				continue
			}
			found = true
			if condition.Status != value.status || condition.Acknowledgement(pod.Generation) != evidence.GenerationCurrent {
				t.Fatalf("Ready=%s lacks the expected current acknowledgement", value.status)
			}
		}
		if !found {
			t.Fatal("the kubelet did not publish Ready")
		}
	}
}

func TestCompletedJobPodIsExcludedFromStatefulSetMembership(t *testing.T) {
	page, err := adapter.ProjectPodList(
		kubectl(t, "get", "--raw", "/api/v1/namespaces/client-k134/pods?labelSelector=job-name%3Dbackup"),
		options(),
	)
	if err != nil || len(page.Observations) != 1 {
		t.Fatalf("expected one completed backup Job Pod: projection=%v count=%d", err, len(page.Observations))
	}
	pod := page.Observations[0]
	excluded, err := evidence.PodHasNonStatefulSetController(pod)
	if err != nil || !excluded || pod.Phase.State != evidence.ValueReported || *pod.Phase.Value != "Succeeded" {
		t.Fatal("a completed Job Pod did not establish non-StatefulSet membership")
	}
}
