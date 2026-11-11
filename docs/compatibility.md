# Compatibility and qualification

The console candidate and migrated CLI have bounded operator integration results below. Other capabilities retain their
listed validation levels. Package compilation, synthetic fixtures, and API discovery do not establish compatibility
with an installed operator. Kubernetes 1.34 is the client qualification floor; 1.33 is outside the target.

| Operator source | API group/version | Capability | Validation level | Evidence |
| --- | --- | --- | --- | --- |
| Operator/chart `0.6.1`; Kubernetes `1.36.1` Linux/arm64; client `7926f09468de` | `openbao.org/v1alpha1` | Namespace cluster/empty restore LIST, basic cluster projection, final UID/generation LIST verification, selected healthy-cluster diagnostics | Local isolated operator integration through console candidate `d079e9f`; consumer merge and hosted checks pending | [Console qualification](console-qualification.md) |
| Operator/chart `0.6.1`; Kubernetes `1.36.1` Linux/arm64; client `74412f30a4c0` | `openbao.org/v1alpha1`, `apps/v1`, core `v1` | Discovery, named cluster GET, scoped workload and empty restore LISTs, final UID/generation GET verification, status watch/interruption, denied reads, and incomplete Pending-Pod diagnosis | Local isolated operator integration through CLI implementation `8cb9910`; no healthy-cluster or nonempty restore qualification | [CLI qualification](cli-qualification.md) |
| Kubernetes `1.34.11` Linux/arm64; default feature gates | core `v1` | Current Ready-condition acknowledgement for True and False; completed Job Pod exclusion from StatefulSet membership | Local isolated Kubernetes field qualification; no operator or ownership-chain qualification | [Kubernetes qualification](kubernetes-qualification.md) |
| Not selected | Not applicable | Generation acknowledgement | Synthetic contract fixtures | `evidence/testdata/generations.json` |
| `9671c30623d609f948cf073e167f376de4ea9a85` | `openbao.org/v1alpha1` | Cluster JSON projection, conditions, versions, readiness fields | Source inspected; production fixture/unit tests; no API-server qualification | `adapter/cluster_test.go`, `internal/contracttest`, `docs/extraction.md` |
| Console/CLI source snapshots above; Kubernetes declarations `v0.37.2` | `openbao.org/v1alpha1`, `apps/v1`, core `v1` | Finding identity, coverage, fifteen supported rules, normalized replay | Production tests and contract fixtures; no integration qualification | `diagnose`, `docs/diagnostics.md`, `internal/contracttest` |
| Source snapshots only | `openbao.org/v1alpha1` | Remaining diagnostic rules and optional local text | Test-only specifications and archived consumer package suites | `docs/baselines.md`, `internal/contracttest` |
| `9671c30623d609f948cf073e167f376de4ea9a85` | `openbao.org/v1alpha1` | Backup, upgrade, restore-test, restrictions, read-replica counts, snapshot-restore facts | Source inspected; production fixture/unit tests; no integration qualification | `adapter/observations_test.go`, `docs/extraction.md` |
| `9671c30623d609f948cf073e167f376de4ea9a85` | `openbao.org/v1alpha1` | Native recovery and remote-replication fields assumed by console | Unsupported; no public API definitions | `docs/extraction.md#source-corrections` |
| `9671c30623d609f948cf073e167f376de4ea9a85` | `openbao.org/v1alpha1` | Restore UID association and display selection | Source inspected; production fixtures and contract tests; no integration qualification | `adapter/association.go`, `internal/contracttest` |
| Kubernetes API declarations `v0.37.2`; operator source `9671c306` for reserved annotations | `apps/v1`, core `v1` | StatefulSet/Pod projection and controller ownership chains | Source inspected; production fixtures, replacement tests, fuzz seeds; no API-server qualification | `adapter/workload_test.go`, `docs/extraction.md`, `NOTICE` |
| Explicit operator/Kubernetes projection contracts above | `openbao.org/v1alpha1`, `apps/v1`, core `v1` | Bounded scoped GET/LIST, pagination, UID-pinned named reads | HTTP fixtures and race tests; no real API-server or operator qualification | `collect/resources_test.go`, `adapter/list_test.go` |
| Explicit operator/Kubernetes projection contracts above; Kubernetes API declarations `v0.37.2` | `openbao.org/v1alpha1`, `apps/v1`, core `v1` | Discovery facts, declared related reads, shared budgets, final UID/generation verification | HTTP fixtures, production transcript tests, serialized verification; no real API-server or operator qualification | `adapter/discovery_test.go`, `collect/related_test.go`, `collect/verification_test.go` |

Record each qualified capability against an operator commit or version, installed CRD identity, image digest, library
commit, test command, environment, result, and evidence location. Keep unsupported and untested capabilities identifiable.
Add the migrated API group only when its specific contract and validation evidence are available.

## Consumer collection coverage

The first common candidate is client `1edeecfd35aedcfa88af9fc57bf1d60dca765c3c`, version
`v0.0.0-20261111115206-1edeecfd35ae`. CLI `ff2d9b427152b8d602964605b07f350d3e6e6ef8` and console PR #24 candidate
`19dd2108873add00dcb08384747e75ef8494dea1` pin it. Their fresh-cache independent builds and distribution artifacts pass.
The private CLI repository keeps GitHub Actions disabled. Console hosted security checks and its latest local
healthy-cluster assertion remain unresolved. The pre-release corrections require a new common candidate before tagging.

The earlier [console qualification](console-qualification.md) and [CLI qualification](cli-qualification.md) retain their
original consumer and library commits. Those bounded integration results do not qualify a later candidate automatically.
No common client tag exists.

| Consumer | Declared scope |
| --- | --- |
| Console candidate | Enrolled namespace LISTs of clusters and snapshot restores, followed by a cluster LIST for UID/generation verification; named enrolled controller Deployment GETs. No Pod reads or named cluster GETs; native recovery is unsupported |
| CLI `status` | Discovery and one named cluster GET through the approved `openbao.org/v1alpha1` contract; subsequent polling remains bound to the selected group and UID without rediscovery |
| CLI `doctor` | Status plus bounded namespace StatefulSet and Pod LISTs with the cluster label selector, an unfiltered namespace restore LIST, and a final cluster GET that verifies UID and generation |

Track application scope separately from library capability. Keep one library implementation of each shared projection
and rule without requiring each application to collect every supported evidence type.
