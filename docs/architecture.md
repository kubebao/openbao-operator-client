# Architecture

The library keeps three things apart: resource facts, their interpretation, and application behavior.

| Package | Allowed first-party dependencies | External boundary |
| --- | --- | --- |
| `evidence` | None | Standard-library value types only |
| `adapter` | `evidence` | Approved public API fields and version contracts |
| `collect` | `adapter`, `evidence` | Caller-configured HTTP transport |
| `diagnose` | `evidence` | Supplied policy and reference time; no I/O |

`make architecture` enforces this direction, the import restrictions on pure packages, and the exclusion of operator and
consumer modules. Test imports follow the same direction.

## Responsibilities

- `evidence` defines normalized values and the pure checks on them: validation, generation comparison, freshness, UID
  associations, and target verification. Adapter and diagnosis code call the same association implementation.
- `adapter` validates types, ranges, identifiers, and timestamps at the boundary. It records reported fields separately
  from derived states. Omission rules come from a pinned public API source, never from discovery or an API version alone.
- `collect` receives a caller-configured transport, target, scope, context, and limits. It never broadens the verbs,
  namespaces, API groups, resources, or selectors that the caller declares.
- `diagnose` receives normalized evidence, selected rules, policy, and a reference time. It returns findings and coverage.
  Findings can coexist with insufficient coverage. Empty findings do not mean complete evidence or a healthy cluster.

## Consumer responsibilities

| Consumer | Keeps |
| --- | --- |
| Console | Authorization, enrollment, history comparison, first-seen tracking, persistence, severity mapping, rendering |
| CLI | Command parsing, kubeconfig loading, watch orchestration, output, severity mapping, process exit codes |

Both consumers own transport configuration, credential renewal, and scheduling.

## Tests

`internal/contracttest` holds synthetic fixtures with hand-authored expected results. Its tests call production code.
`internal/integration` holds Kubernetes field checks that run only against a disposable cluster; see
[development](development.md#kubernetes-field-check).

Add an `operations` or bundle package together with its first implemented feature.
