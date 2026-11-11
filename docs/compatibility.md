# Compatibility

## Kubernetes

The client supports Kubernetes 1.34 and later. Pod readiness evaluation needs the kubelet to report `observedGeneration`
on Pod conditions. `PodObservedGenerationTracking` does that and is enabled by default from 1.34. If it is disabled, Pod
readiness coverage is `insufficient` with `generation_unconfirmed`. A `Failed` phase still produces a finding.

## Contracts

| Contract | Source | Validation |
| --- | --- | --- |
| `openbao.org/v1alpha1@9671c306/maintenance-pools-v1` | Operator commit `9671c30623d609f948cf073e167f376de4ea9a85` | Source inspected; unit and fixture tests; local operator integration with `0.6.1` |
| `openbao.org/v1alpha1/unqualified-v3` | Same fields, no omission rules | Unit and fixture tests |
| `kubebao.org/v1alpha1@22894cdc/maintenance-pools-v1` | Native operator candidate `22894cdc0a4aa5c442a07f2bf9902fe28ce02ea1` | Source inspected; unit and fixture tests; local operator integration with this candidate |
| `kubebao.org/v1alpha1/unqualified-v1` | Same native fields, no omission rules | Unit and fixture tests |
| `kubernetes/apps/v1@v0.37.2/workload-pools-v1` | Kubernetes API declarations `v0.37.2`; operator workload metadata at `9671c306` | Source inspected; unit and fixture tests; local operator integration |
| `kubernetes/core/v1@v0.37.2/workload-pools-v1` | Kubernetes API declarations `v0.37.2`; operator workload metadata at `9671c306` | Source inspected; unit and fixture tests; local operator integration |

Validation levels mean:

- **Unit and fixture tests**: synthetic input. They do not cover API-server defaulting, admission, RBAC, or reconciliation.
- **Kubernetes field check**: real kubelet and Job controller fields on Kubernetes 1.34.11. This does not cover the
  operator.
- **Local observation integration**: the console and CLI ran against operator `0.6.1` on Kubernetes 1.36.1 in an isolated
  cluster. This covered cluster GET and LIST, UID-associated StatefulSet and Pod observations, empty restore LISTs,
  target verification, denied reads, healthy selected diagnostics, and status-watch interruption. A completed Job Pod
  with the cluster label was excluded from StatefulSet membership. This evidence covers the schema `2` observation
  contract. It does not qualify schema `4`, control submission, maintenance propagation assessment, non-empty restores,
  or every rule.
- **Local operator integration** for schema `4`: the library ran against released operator `0.6.1` (source
  `5b5470714cfa2ff4372634e229c77b55be8f65b3`) and the native candidate `22894cdc` in separate disposable Kind clusters.
  Both used Kubernetes 1.36.1, Linux/arm64, OpenBao 2.7.2, the Development profile, self-initialization, and enabled
  admission policies. Each environment covered one voter, one voter plus one read replica, a configured zero read pool,
  and an initialized revisioned blue/green voter pool with reported `Idle` state. This qualifies the control and workload
  observations listed below for these fixtures. It does not qualify another operator build, a completed upgrade, HA,
  production operation, or control behavior on Kubernetes 1.34.

Package compilation, fixtures, and API discovery do not establish compatibility with an installed operator.

## Cluster controls

The `operations` package prepares and submits pause/resume and maintenance requests under either inspected source
contract. It supports one JSON Patch request and optional server dry-run. Local operator integration covers server
dry-run without persistence, RBAC denial, pre-transport cancellation, failed UID/resource-version/generation patch tests,
and a discarded response after one accepted PATCH. A later read confirms the requested state after the unknown outcome;
the client does not resubmit. A wrong-UID patch test does not qualify an actual delete-and-recreate replacement.
Server dry-run acceptance does not prove that a later request will be accepted.

Evidence schema `4` and the current operator and workload contract identifiers are required. Earlier serialized captures
must be recollected. Consumer release pins are managed in their repositories.

Maintenance propagation assessment covers one stable voter pool and a configured read-replica pool, including a
zero-replica pool. A settled blue/green deployment needs a valid `Idle` phase and consistent stable-revision evidence.
Active blue/green transitions remain insufficient. Local operator integration verifies pause/resume acknowledgement on
the voter-only fixture and maintenance enable/disable propagation on all four topologies, including the revisioned initial blue/green pool.
It does not execute a blue/green upgrade or qualify active transitions. Unit and fixture tests cover unsupported and
incomplete observations.

The native contract preserves the operator's `openbao.org/*` metadata keys. It does not establish compatibility with a
released `kubebao.org` operator. Consumers must select a contract; matching discovery alone is not qualification.

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
