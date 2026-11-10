# Architecture

The library separates resource facts from their interpretation and from application behavior.

| Package | Allowed first-party dependencies | External boundary |
| --- | --- | --- |
| `evidence` | None | Standard-library value types only |
| `adapter` | `evidence` | Approved public API fields and version contracts |
| `collect` | `adapter`, `evidence` | Caller-configured Kubernetes readers |
| `diagnose` | `evidence` | Supplied policy and reference time; no network access |

`make architecture` enforces package direction, pure-package import restrictions, and exclusion of operator and consumer
modules. Unit-test imports follow the same package direction. Collector tests can use Kubernetes clients and test servers.

Capability definitions belong to adapters. Evidence records observed discovery/read outcomes and the contract under
which a projection was made. Diagnosis consumes those value types without importing adapter code.

Adapters validate types, ranges, identifiers, and timestamps at the boundary. Preserve reported fields separately from
derived states. Document zero-value omission semantics against a pinned public operator API/CRD source. A resource API
version or discovery response alone does not establish compatibility.

Collection receives a caller-configured reader, target, scope, context, and limits. Keep kubeconfig selection, transport
policy, credential renewal, enrollment, scheduling, and persistence in applications. Shared primitives must not expand a
consumer's verbs, namespaces, API groups, resources, selectors, or permissions.

Diagnosis receives normalized evidence, selected rules, evaluation policy, and reference time. It returns findings and
coverage. Threshold-to-exit-code mapping and presentation remain consumer responsibilities. Findings can coexist with
insufficient evidence. Empty findings do not imply complete evidence or a healthy cluster.

The console keeps authorization, history comparison, first-seen tracking, and persistence. The CLI keeps command parsing,
kubeconfig loading, output, watch orchestration, and process results. Application mappings can change during initial
development when the new contract improves semantics; document storage and output changes.

Only generation acknowledgement is implemented. Remaining packages establish boundaries without claiming working API
adapters, collectors, or rules. Add `operations` and bundle packages with their first implemented features.

`internal/contracttest` contains test-only executable specifications and synthetic expected-result fixtures. Its archived
consumer harnesses run separately against immutable source snapshots. Milestone 3 replaces the reference models with
production calls without changing expected results unless a contract decision is reviewed and documented.
