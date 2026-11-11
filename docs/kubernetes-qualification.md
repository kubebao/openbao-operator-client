# Kubernetes field qualification

The client targets Kubernetes 1.34 and later. This test qualifies specific Pod fields on Kubernetes 1.34.11 with default
feature gates. It does not qualify an operator integration, workload ownership chains, or every Kubernetes version.

The test checks that the kubelet publishes current condition acknowledgement for both `Ready=True` and `Ready=False`.
It also projects a completed Job Pod and checks that its single Job controller reference excludes StatefulSet membership.
Unit tests separately verify findings, ambiguous references, and Pod population counts. Missing condition acknowledgement
still limits diagnostic coverage; no omitted generation is inferred.

## Reproduce

Use kind 0.33.0, kubectl, and the pinned Go toolchain. Use a disposable cluster and its own kubeconfig. The fixture
contains two pause Pods and one Job. It does not install the operator or read Secrets.

```sh
mkdir -p .local/kubernetes-1.34
kind create cluster --name client-k134 \
  --image kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d \
  --kubeconfig .local/kubernetes-1.34/admin-kubeconfig
kubectl --kubeconfig .local/kubernetes-1.34/admin-kubeconfig create namespace client-k134
kubectl --kubeconfig .local/kubernetes-1.34/admin-kubeconfig \
  apply -f internal/integration/testdata/workloads.yaml
kubectl --kubeconfig .local/kubernetes-1.34/admin-kubeconfig -n client-k134 \
  wait --for=condition=Ready pod/ready-true --timeout=60s
kubectl --kubeconfig .local/kubernetes-1.34/admin-kubeconfig -n client-k134 \
  wait --for=jsonpath='{.status.phase}'=Running pod/ready-false --timeout=60s
kubectl --kubeconfig .local/kubernetes-1.34/admin-kubeconfig -n client-k134 \
  wait --for=condition=Complete job/backup --timeout=60s
OPENBAO_CLIENT_TEST_KUBECONFIG="$PWD/.local/kubernetes-1.34/admin-kubeconfig" \
  GOWORK=off go test -v -count=1 ./internal/integration
```

Both tests must pass. Without `OPENBAO_CLIENT_TEST_KUBECONFIG`, the tests skip and establish no qualification.
The harness uses kubectl only in the integration-test package. Public client packages do not load kubeconfig or start
processes. The test performs fixed reads in `client-k134` and reads the server version. Its administrator kubeconfig
does not establish a minimum-permission qualification.

Remove only this disposable cluster after testing:

```sh
kind delete cluster --name client-k134 --kubeconfig .local/kubernetes-1.34/admin-kubeconfig
```

## Recorded result

Local Linux/arm64 Kubernetes 1.34.11 on kind 0.33.0 passes both tests on 11 November 2026. The Node image and pause image
are pinned by digest in the commands and fixture. No feature-gate override or status patch is used. The test uses the
actual kubelet conditions and the Job controller's owner reference.
