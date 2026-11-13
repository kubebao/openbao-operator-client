# Compatibility

## Kubernetes

The client supports Kubernetes 1.34 and later. Pod readiness evaluation needs the kubelet to report `observedGeneration`
on Pod conditions. `PodObservedGenerationTracking` does that and is enabled by default from 1.34. If it is disabled, Pod
readiness coverage is `insufficient` with `generation_unconfirmed`. A `Failed` phase still produces a finding.

## Release compatibility

The Go API and diagnostic semantics can change between `v0` releases. Consumers update to a qualified client commit
together. The client does not guarantee compatibility for earlier evidence JSON or diagnostic policy. Consumer database
formats and user data are separate responsibilities; changing the client does not authorize resetting them.

## Adapter selection

| Contract | Source | Validation |
| --- | --- | --- |
| `openbao.org/v1alpha1` | Operator commit `9671c30623d609f948cf073e167f376de4ea9a85` | Source inspected; unit and fixture tests; local controls and restore observation integration with `0.6.1` |
| `openbao.org/v1alpha1/unqualified` | Reported fields without omission rules or restore UID bindings | Unit and fixture tests |
| `kubebao.org/v1alpha1` | Native operator candidate `22894cdc0a4aa5c442a07f2bf9902fe28ce02ea1` | Source inspected; unit and fixture tests; local control integration with this candidate and restore observations with edge `587dd163` |
| `kubebao.org/v1alpha1/unqualified` | Reported native fields without omission rules or restore UID bindings | Unit and fixture tests |
| `apps/v1` | Kubernetes API declarations `v0.37.2`; operator workload metadata at `9671c306` | Source inspected; unit and fixture tests; local operator integration |
| `v1` | Kubernetes API declarations `v0.37.2`; operator workload metadata at `9671c306` | Source inspected; unit and fixture tests; local operator integration |

Validation levels mean:

- **Unit and fixture tests**: synthetic input. They do not cover API-server defaulting, admission, RBAC, or reconciliation.
- **Kubernetes field check**: real kubelet and Job controller fields on Kubernetes 1.34.11. This does not cover the
  operator.
- **Local observation integration**: the console and CLI ran against operator `0.6.1` on Kubernetes 1.36.1 in an isolated
  cluster. This covered cluster GET and LIST, UID-associated StatefulSet and Pod observations, empty restore LISTs,
  target verification, denied reads, healthy selected diagnostics, and status-watch interruption. A completed Job Pod
  with the cluster label was excluded from StatefulSet membership. This covers observation field behavior. It does not
  qualify control submission, maintenance propagation assessment, nonempty restores, or every rule.
- **Local operator integration**: the library ran against released operator `0.6.1` (source
  `5b5470714cfa2ff4372634e229c77b55be8f65b3`) and the native candidate `22894cdc` in separate disposable Kind clusters.
  Both used Kubernetes 1.36.1, Linux/arm64, OpenBao 2.7.2, the Development profile, self-initialization, and enabled
  admission policies. Each environment covered one voter, one voter plus one read replica, a configured zero read pool,
  and an initialized revisioned blue/green voter pool with reported `Idle` state. This qualifies the control and workload
  observations listed below for these fixtures. It does not qualify another operator build, a completed upgrade, HA,
  production operation, or control behavior on Kubernetes 1.34.

These environment checks establish public API field and control behavior. They do not qualify every later client
commit. Releases require both consumers to be qualified against the exact candidate. Package compilation, fixtures,
and API discovery do not establish compatibility with an installed operator.

## Restore observations

Local restore observation integration covers the public fields used for nonempty restore lists:

- Native operator edge commit `587dd16386bc5a234520e22eec1bbedb904fa6dd`, Kubernetes `1.36.1+k3s1`, Linux/amd64, and
  OpenBao `2.7.2` in the migrated demo. Reads cover an existing-target `Unknown` request bound to the current migrated
  cluster UID, and a fresh single-voter disposable target. A successful executor and reported application remain
  `Unknown` during retention; diagnosis reports informational cleanup retention only with the required evidence.
  A short-retention request reaches `Completed` after the original target, PVC, and Job disappear. Its retained receipt
  does not support current findings for the absent target.
- Released legacy operator `0.6.1`, Kubernetes `1.36.1`, Linux/arm64, OpenBao `2.7.2`, and a one-voter Development fixture
  in an isolated Kind cluster. The harness takes a real RustFS-backed snapshot, changes a sample value, restores that
  snapshot into its original fixture, and verifies the restored value. The operator retains `Unknown` and its hold;
  diagnosis retains the recovery concern. No automatic retry or recovery acknowledgement is performed.

The observer uses public projections, complete bounded reads, UID association, and target verification. Fixture setup,
snapshot submission, sample-data access, and cleanup belong to the test harness. Synthetic cases cover malformed or
missing receipts, failed cleanup, ambiguous or conflicting identity, incomplete reads, and API-group isolation. They do
not establish runtime coverage for those failure modes. Exact image, CRD, source, and command identities belong in the
qualification pull request.

Native application evidence is the operator's recorded identity, version, and health observation. The separate sample
value check applies to the legacy fixture. This qualification does not establish HA, production recovery, application
logins, external fencing, or general backup/restore execution APIs in the shared client.

## Cluster controls

The `operations` package prepares and submits pause/resume and maintenance requests under either inspected source
contract. It supports one JSON Patch request and optional server dry-run. Local operator integration covers server
dry-run without persistence, RBAC denial, pre-transport cancellation, failed UID/resource-version/generation patch tests,
and a discarded response after one accepted PATCH. A later read confirms the requested state after the unknown outcome;
the client does not resubmit. A wrong-UID patch test does not qualify an actual delete-and-recreate replacement.
Server dry-run acceptance does not prove that a later request will be accepted.

The client release defines the current typed model. It has no independent evidence schema or rule-set counter and no
reader for earlier serialized models. Consumers pin the client and own stored-format rejection. Existing public tags
remain immutable.

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
