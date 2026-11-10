# Milestone 3 extraction status

The first production slice implements cluster projection and the evidence values needed to preserve the contract.
Milestone 3 remains in progress. Neither consumer imports the library yet.

| Slice | State | Verification |
| --- | --- | --- |
| Resource identities, references, field provenance, generations, condition sets, timestamps, collection outcomes | Implemented | Production unit tests and unchanged milestone 1 value/condition/time fixtures |
| Approved cluster JSON projection, versions, readiness counts, pause spec value, contract descriptions | Implemented | Source-pinned adapter tests, canaries, malformed input, bounded parsing, serialization, and fuzz seeds |
| Backup, upgrade, replication, restore-test, restore and native recovery observations | Pending | Archived consumer baselines and reference specifications |
| StatefulSet/Pod evidence, UID associations, restore correlation | Pending | Reference specifications and archived CLI safeguards |
| Bounded discovery, reads, pagination, cancellation, and replacement verification | Pending | Transcript specifications; no production reader yet |
| Findings, coverage, rule catalog, evaluation policy, deterministic diagnostic replay | Pending | Reference specifications; no production rules yet |

## Cluster projection

`adapter.ProjectCluster` accepts JSON bytes and `ProjectionOptions`. Supply a stable origin, collection time, explicit
contract, and positive projection limits. It does not load Kubernetes configuration or perform reads. The default limits
are 1 MiB of input and 256 conditions. JSON nesting is limited to 64 levels below the root. Duplicate object keys, invalid UTF-8, trailing
documents, invalid resource identity, and a mismatched kind/API version reject the projection. Errors expose a bounded
`ProjectionError.Code`; they do not include raw parser errors, field values, or response bodies.

Malformed optional fields retain an invalid state while independent facts remain available. Invalid condition type,
status, observed generation, or duplicate types invalidate the condition set. An invalid optional reason or transition
time remains a field-level invalid value. A condition-count limit invalidates the set and preserves independent fields.
An input byte/depth limit rejects the document before any partial resource identity is returned.

Contracts are selected with `adapter.V1Alpha1Snapshot` or `adapter.V1Alpha1Unqualified`. The snapshot contract implements
the documented omitted-zero/false policy from operator source `9671c30623d609f948cf073e167f376de4ea9a85`. The unqualified
contract projects the same approved fields without inferring omitted values. Other contract IDs and API groups are
rejected. `adapter.Describe` reports implemented projection capabilities and source provenance; it does not report API
discovery, RBAC access, or operator integration qualification.

Version fields use the pinned public `spec.version` semantic-version pattern, including optional `v`, prerelease, and build
metadata. The projection records reported counts and generation acknowledgement separately. Stale counts remain reported
facts; the adapter never converts them into health. Provider messages, specs beyond approved fields, arbitrary annotations,
and raw errors are absent from the resulting type. Optional local text remains unimplemented.

## Evidence and replay

Value states distinguish unknown, reported, implied, and invalid. Invalid fields retain no raw value. Implied integers must
be zero; implied booleans must be false. Adapters determine whether the selected field and contract permit inference.
`Validate` checks normalized shapes after construction or JSON decoding. The zero state is invalid: consumers must declare
unknown fields rather than silently defaulting them. Cluster validation does not select an adapter or qualify its contract.

References retain full resource identity, API version, resource version, read time, and an optional field selector. Identity
components are bounded to 253 UTF-8 bytes and reject controls and Unicode replacement characters. Values are never trimmed
or cleaned into a different identity. The identity checks do not replace Kubernetes schema validation.

Collection intervals, producer timestamps, creation timestamps, and condition transitions remain distinct. Freshness
assessment receives the analysis reference time and age limit; it reads no clock and accepts no clock skew. A valid complete
empty list establishes absence only within its declared scope. Named GET and discovery results never establish list-scope
absence. `Collection.Items` counts observed candidates before association filtering.

`internal/contracttest` now calls production code for value, condition, identity-validation, and freshness decisions.
Field-focused fixtures receive synthetic resource metadata in test setup; the production API always requires identity.
Expected results are unchanged. Remaining reference models stay until their extraction slice replaces them.

Run `go run ./examples/cluster` for a synthetic projection. It prints current generation acknowledgement and an implied
zero ready count. This is resource evidence, not a health check. No API-server or operator integration is qualified.
