# Milestone 3 extraction status

Production slices implement cluster projection, operational observations, and evidence values. Milestone 3 remains in
progress. Neither consumer imports the library yet.

| Slice | State | Verification |
| --- | --- | --- |
| Identities, references, field provenance, generations, conditions, timestamps, collection outcomes | Implemented | Production tests and milestone 1 fixtures |
| Cluster JSON projection, versions, readiness counts, pause, contract descriptions | Implemented | Source-pinned tests, canaries, limits, serialization, fuzz seeds |
| Backup, rolling/blue-green upgrade, restore-test, restrictions, read-replica counts, snapshot-restore recovery facts | Implemented | Production fixtures, malformed-field tests, serialized replay |
| Native `OpenBaoRecovery` and remote-replication observations | Unsupported by this contract | No definitions in the pinned public API |
| StatefulSet/Pod evidence, UID associations, restore correlation | Pending | Reference specifications and archived CLI safeguards |
| Bounded discovery, reads, pagination, cancellation, replacement verification | Pending | Transcript specifications; no production reader yet |
| Findings, coverage, evaluation policy, deterministic diagnostic replay | Pending | Reference specifications; no production rules yet |

## Projection boundary

`adapter.ProjectCluster` and `adapter.ProjectRestore` accept JSON bytes and `ProjectionOptions`. Supply a stable origin,
collection time, explicit contract, and positive limits. They perform no reads and use no wall clock. Default limits are
1 MiB per document, 256 conditions per set, and 256 entries per ordinal set. Nesting is limited to 64 levels below the root.
Duplicate keys, invalid UTF-8, trailing documents, invalid identity, and mismatched kind/API version reject the document.
Errors expose a bounded `ProjectionError.Code` without raw parser errors, source values, or response bodies.

Malformed optional fields retain invalid states while independent facts remain available. Invalid condition types,
statuses, observed generations, or duplicate types invalidate the condition set. Invalid optional reasons and transition
times remain field-level invalid values. Set limits invalidate the set and preserve independent fields. Document byte/depth
limits reject the projection before returning partial resource identity.

Choose `adapter.V1Alpha1Snapshot` or `adapter.V1Alpha1Unqualified`. The snapshot implements the documented omitted-zero/false
policy from source `9671c30623d609f948cf073e167f376de4ea9a85`. The unqualified contract projects approved fields without
inferring omitted values. Other contract IDs and API groups are rejected. `adapter.Describe` reports projection coverage
and provenance. It does not establish API discovery, RBAC access, or integration qualification.

Version fields follow the pinned public semantic-version pattern, including optional `v`, prerelease, and build metadata.
Counts and generation acknowledgement remain separate facts. Stale counts remain reported; projection never converts them
into health. Optional local text remains unimplemented.

## Operational observations

Backup observations keep completion, attempt, and failure times separate. Sizes remain exact integers; snapshot keys and
digests are validated. Missing failure counters stay unknown. Restore tests retain active-object presence, historical
outcomes, success time, and conditions independently. Running, blocked, and failed interpretations belong to diagnosis.

Upgrade evidence includes execution identity, versions, partition, sorted unique completed ordinals, and structured errors.
Blue/green evidence includes phase, operation identity, failed Job name, and failure count. Restriction evidence includes
pause, maintenance, restore origin/hold, operation lock, and break-glass state. Provider messages, acknowledgment nonces,
recovery instructions, and free-form rollback text are excluded. Controller error reasons and their timestamps remain
available independently.

Restore projection keeps phase, execution stage, submission-claim presence, reported target application time, cleanup,
administrator disposition, and managed restart completion separate. It does not infer pending from age, choose a matching
UID over a conflicting UID, or verify lifecycle actions. Legacy restart metadata is historical evidence, not ownership.

Optional objects distinguish unknown ancestor, absent field, explicit null, reported object, and malformed object. Null
objects retain no child facts. Absence is not an implied zero, false, healthy state, or completed operation. Primitive nulls
remain invalid. This preserves nullable public status objects without flattening them into counter defaults.

### Source corrections

The pinned operator defines neither `OpenBaoRecovery` nor the console's `spec.remoteReplica` and
`status.remoteReplicaObservation` fields. It also does not define an execution stage named `RestoreSubmitted`. Those console
paths are synthetic assumptions, not public API contracts. The adapter rejects the native kind, omits remote fields, and
marks the non-API stage invalid. Native association fixtures now require unconfirmed results under this contract.

Native recovery and remote history-conflict rule IDs remain reserved inventory. They must not produce findings for this
contract. The console migration must remove or capability-gate these paths and their reader permissions. Adding those
features requires public API definitions and separate contract qualification. Current read-replica counts describe the
pool inside one OpenBao cluster; they do not qualify remote replication.

## Evidence and replay

Normalized cluster/restore records use schema version `2`. Operational fields and object-presence states change the draft
version `1` serialization; validation rejects older records. Neither consumer persists library records yet. Evidence schema,
adapter contract identity, and future rule-set versions remain separate.

Value states distinguish unknown, reported, implied, and invalid. Invalid fields retain no raw value. Implied integers must
be zero; implied booleans must be false. Adapters determine whether a field and contract permit inference. `Validate` checks
normalized shapes after construction or JSON decoding. The zero state is invalid: consumers must declare unknown fields.
Cluster validation does not select an adapter or qualify its contract.

References retain full identity, API version, resource version, read time, and optional field selector. Identity components
are bounded to 253 UTF-8 bytes and reject controls and Unicode replacement characters. They are never trimmed into a
different identity. These checks do not replace Kubernetes schema validation.

Collection intervals, producer times, creation times, and condition transitions remain separate. Freshness receives an
analysis reference time and age limit, reads no clock, and permits no clock skew. Only valid complete empty lists establish
absence within their declared scope. Named GET and discovery do not establish list-scope absence. `Collection.Items`
counts candidates before association filtering.

Value, condition, identity-validation, and freshness fixtures call production code. Field-focused fixtures receive synthetic
metadata in test setup; production always requires identity. Expected results are unchanged except for the documented
unsupported-native correction. Remaining reference models stay until their supported extraction slice replaces them.
Remote-index fixtures remain future specifications, not an API adapter.

Run `go run ./examples/cluster` for a synthetic projection. Current acknowledgement and an implied zero ready count are
resource evidence, not a health check. No API-server or operator integration is qualified.
