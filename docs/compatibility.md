# Compatibility and qualification

No operator capability is integration-qualified. Package compilation, synthetic fixtures, and API discovery do not
establish compatibility with an installed operator.

| Operator source | API group/version | Capability | Validation level | Evidence |
| --- | --- | --- | --- | --- |
| Not selected | Not applicable | Generation acknowledgement | Synthetic contract fixtures | `evidence/testdata/generations.json` |
| `9671c30623d609f948cf073e167f376de4ea9a85` | `openbao.org/v1alpha1` | Cluster JSON projection, conditions, versions, readiness fields | Source inspected; production fixture/unit tests; no API-server qualification | `adapter/cluster_test.go`, `internal/contracttest`, `docs/extraction.md` |
| Console/CLI source snapshots above | `openbao.org/v1alpha1` | Finding identity, coverage, twelve cluster rules, normalized replay | Production tests and contract fixtures; no integration qualification | `diagnose`, `docs/diagnostics.md`, `internal/contracttest` |
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

Neither consumer imports this module yet. These are migration boundaries from the baseline source, not library qualification.

| Consumer | Declared scope |
| --- | --- |
| Console | Enrolled namespace lists of clusters and optional restore/recovery requests; named enrolled controller Deployment reads |
| CLI `status` | One named cluster and API discovery; subsequent polling remains bound to the selected group and UID |
| CLI `doctor` | Status plus bounded StatefulSet, Pod, and restore candidate reads with identity verification |

Track application scope separately from library capability. Keep one library implementation of each shared projection
and rule without requiring each application to collect every supported evidence type.
