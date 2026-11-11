# Compatibility

## Kubernetes

The client supports Kubernetes 1.34 and later. Pod readiness evaluation needs the kubelet to report `observedGeneration`
on Pod conditions. `PodObservedGenerationTracking` does that and is enabled by default from 1.34. If it is disabled, Pod
readiness coverage is `insufficient` with `generation_unconfirmed`. A `Failed` phase still produces a finding.

## Contracts

| Contract | Source | Validation |
| --- | --- | --- |
| `openbao.org/v1alpha1@9671c306/maintenance-pools-v1` | Operator commit `9671c30623d609f948cf073e167f376de4ea9a85` | Source inspected; unit and fixture tests |
| `openbao.org/v1alpha1/unqualified-v3` | Same fields, no omission rules | Unit and fixture tests |
| `kubernetes/apps/v1@v0.37.2/workload-pools-v1` | Kubernetes API declarations `v0.37.2`; operator workload metadata at `9671c306` | Source inspected; unit and fixture tests |
| `kubernetes/core/v1@v0.37.2/workload-pools-v1` | Kubernetes API declarations `v0.37.2`; operator workload metadata at `9671c306` | Source inspected; unit and fixture tests |

Validation levels mean:

- **Unit and fixture tests**: synthetic input. They do not cover API-server defaulting, admission, RBAC, or reconciliation.
- **Kubernetes field check**: real kubelet and Job controller fields on Kubernetes 1.34.11. This does not cover the
  operator.
- **Local operator integration**: the console and CLI ran against operator `0.6.1` on Kubernetes 1.36.1 in an isolated
  cluster. This covered cluster GET and LIST, UID-associated StatefulSet and Pod observations, empty restore LISTs,
  target verification, denied reads, healthy selected diagnostics, and status-watch interruption. A completed Job Pod
  with the cluster label was excluded from StatefulSet membership. This evidence covers the schema `2` observation
  contract. It does not qualify schema `4`, control submission, maintenance propagation assessment, non-empty restores,
  or every rule.

Package compilation, fixtures, and API discovery do not establish compatibility with an installed operator.

## Cluster controls

The `operations` package prepares and submits pause/resume and maintenance requests under the qualified source contract.
It supports one JSON Patch request and optional server dry-run. Source inspection, unit tests, and synthetic HTTP tests
cover these methods. They have no operator integration qualification. Server dry-run acceptance does not qualify runtime
behavior or prove that a later request will be accepted.

Evidence schema `4` and the current operator and workload contract identifiers are required. Earlier serialized captures
must be recollected. Consumer release pins are managed in their repositories.

Maintenance propagation assessment covers one stable voter pool and a configured read-replica pool, including a
zero-replica pool. A settled blue/green deployment needs a valid `Idle` phase and consistent stable-revision evidence.
Active blue/green transitions remain insufficient. Unit and fixture tests cover these interpretations; no live operator
qualification covers workload-pool or revision assessment.

Restart, backup, upgrade, restore, recovery acknowledgements, scaling, and general configuration writes are not exposed
through this package.

## Unsupported

The supported operator contract has no public API for native recovery (`OpenBaoRecovery`) or remote replication. The
adapter rejects the `OpenBaoRecovery` kind, and the reserved rules `KBAO-RECOVERY-001` and `KBAO-REPLICATION-001` produce
no findings.

## Consumer read scopes

The library never broadens a consumer's reads. Each consumer declares these scopes:

| Consumer | Reads |
| --- | --- |
| Console | Namespace LISTs of clusters and snapshot restores in enrolled namespaces, then a cluster LIST for target verification. Named GETs of enrolled controller Deployments. No Pod reads and no named cluster GETs. |
| CLI `status` | Discovery and one named cluster GET. Polling stays bound to the selected group and UID. |
| CLI `doctor` | `status`, plus StatefulSet and Pod LISTs with `openbao.org/cluster=<name>`, an unfiltered restore LIST, and a final cluster GET for target verification. |
