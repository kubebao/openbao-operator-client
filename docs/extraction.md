# Milestone 3 extraction status

Production slices implement cluster/workload projection, operational observations, evidence values, UID associations,
bounded discovery, GET/LIST reads, final target verification, and the diagnostic core.
All supported milestone 3 production slices are implemented. Final candidate qualification requires the published module
build and hosted required checks. Neither consumer imports the library yet; migration and integration qualification remain
in milestones 4 through 6.

| Slice | State | Verification |
| --- | --- | --- |
| Identities, references, field provenance, generations, conditions, timestamps, collection outcomes | Implemented | Production tests and milestone 1 fixtures |
| Cluster JSON projection, versions, readiness counts, pause, contract descriptions | Implemented | Source-pinned tests, canaries, limits, serialization, fuzz seeds |
| Backup, rolling/blue-green upgrade, restore-test, restrictions, read-replica counts, snapshot-restore recovery facts | Implemented | Production fixtures, malformed-field tests, serialized replay |
| Native `OpenBaoRecovery` and remote-replication observations | Unsupported by this contract | No definitions in the pinned public API |
| StatefulSet/Pod evidence, UID associations, restore selection | Implemented | Production fixtures, replacement/conflict tests, milestone 1 specifications |
| Scoped GET/LIST reads, pagination, cancellation, UID-pinned named refresh | Implemented | HTTP fixtures, limits, partial reads, scope and replacement tests |
| Bounded discovery and final target verification across declared related reads | Implemented | HTTP fixtures, shared budgets, verification states, production transcript tests |
| Finding identity, coverage, evaluation policy, deterministic diagnostic replay | Implemented for cluster inputs | Production tests and milestone 1 fixtures |
| Availability, sealing, readiness, conditions, generations, controller errors, restrictions, backup concerns, upgrade failure, restore tests | Implemented in rule set `6` | Source-pinned semantics, boundaries, current/partial/stale evidence tests |
| StatefulSet and Pod readiness, failure, and population | Implemented in rule set `6` | UID chains, per-scope outcomes, final verification, deterministic replay, synthetic collection-to-analysis test |
| Snapshot-restore progress and recovery concerns | Implemented in rule set `6` | UID-binding capability policy, held/historical distinctions, independent receipts, deterministic replay, synthetic collection-to-analysis tests |

## Projection boundary

`adapter.ProjectCluster`, `ProjectRestore`, `ProjectStatefulSet`, and `ProjectPod` accept JSON bytes and `ProjectionOptions`.
Supply a stable origin, collection time, explicit contract, and positive limits. They perform no reads and use no wall clock.
Default limits are 1 MiB per document, 256 conditions per set, and 256 entries per ordinal, owner, or container set.
Nesting is limited to 64 levels below the root.
Duplicate keys, invalid UTF-8, trailing documents, invalid identity, and mismatched kind/API version reject the document.
Errors expose a bounded `ProjectionError.Code` without raw parser errors, source values, or response bodies.

Malformed optional fields retain invalid states while independent facts remain available. Invalid condition types,
statuses, observed generations, or duplicate types invalidate the condition set. Invalid optional reasons and transition
times remain field-level invalid values. Set limits invalidate the set and preserve independent fields. Document byte/depth
limits reject the projection before returning partial resource identity.

Choose `adapter.V1Alpha1Snapshot` or `adapter.V1Alpha1Unqualified`. The snapshot implements the documented omitted-zero/false
policy from source `9671c30623d609f948cf073e167f376de4ea9a85`. The unqualified contract projects approved fields without
inferring omitted values. Workload projection uses separate Kubernetes contracts described below. Each contract restricts
the accepted API version and kinds. Other contract IDs and API groups are rejected. `adapter.Describe` reports projection
coverage and provenance. It does not establish API discovery, RBAC access, or integration qualification.

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

## Workloads and associations

`adapter.KubernetesAppsV1` permits only `apps/v1` StatefulSets. `adapter.KubernetesCoreV1` permits only `v1` Pods. These
contracts reference public Kubernetes API declarations at `v0.37.2`, matching the consumers' pinned dependencies.
Projection uses the standard library; no Kubernetes dependency or client is introduced.

StatefulSet evidence retains desired/ready counts and resource acknowledgement separately. The pinned status declaration
uses a non-pointer `omitempty` ready count. Omission implies zero only with positive current acknowledgement and a known
desired count. Missing desired replicas remain unknown; projection does not apply API-server defaults.

Pod evidence retains phase, conditions, deletion time, maintenance annotation, and bounded container statuses. Init and
application container sets retain their presence independently. Containers are sorted by name within each set. Duplicate
names invalidate that set. Termination codes retain the full signed `int32` range; they are workload facts, separate from
CLI process results. Specs, environment variables, runtime messages, and arbitrary annotations are excluded.

`AssociateStatefulSet` requires one controller owner reference matching the cluster's group, kind, name, and UID.
`AssociatePod` requires the same match against a StatefulSet already confirmed to the cluster. API version differences
within one owner group do not change identity. Both edges stay within the origin and namespace. The reserved
`openbao.org/owner-uid` annotation refers to the cluster UID even on Pods. A conflicting or malformed annotation blocks
confirmation; annotation matches alone prove no ownership. Replacements cannot reuse an old parent association.

`AssociateRestore` retains the exact UID confirmation basis and checks every supplied target name and target/execution
UID before confirming. Malformed binding objects or fields prevent confirmation. Names alone retain candidates; hold
operation IDs and creation times establish no ownership. `LatestRestore` selects only confirmed requests with known
creation times at or before their read time, sorts by newest time then UID ascending, and rejects duplicate identities.
Selection is for display; it does not verify acceptance, causality, or operation success.

## Bounded resource reads

`collect.NewClient` receives an API-server endpoint and a caller-configured `http.RoundTripper`. The caller owns TLS,
authentication, credential renewal, proxy policy, and transport rate limits. The transport must honor request contexts
and must not follow redirects. The library performs GET requests directly through that transport. It loads no credentials,
follows no redirects, retries no requests, and selects no alternative API group. Endpoint path prefixes are preserved;
userinfo, query strings, fragments, encoded prefixes, and traversal segments are rejected.

`Client.Clusters`, `Restores`, `StatefulSets`, and `Pods` accept an exact `evidence.Scope`, adapter contract, projection
limits, and collection limits. Each method permits only its resource and declared contract. A named GET requires a
namespace. An all-namespace LIST also requires `AllowAllNamespaces`; no read automatically broadens the scope. Label
selectors pass unchanged as encoded query values. The cluster method adds no Pod, StatefulSet, restore, or discovery reads.

Default limits are 8 requests, 500 candidates, 100 candidates per requested page, 1 MiB per response, 8 MiB across response
bodies, 8 MiB of serialized observations, and 15 seconds across one GET or LIST. Every limit must be positive. Page size
cannot exceed candidate or projection entry limits. A shorter caller deadline takes precedence. Response reading uses
one lookahead byte to detect overflow and closes the body before projection. `Result.Bytes` counts body bytes read,
including partial or oversized bodies; the final lookahead can exceed the body budget by one byte. A failed or oversized
response stops the sequence. `RetainedBytes` sums serialized observation sizes, excluding the result envelope. Counts and
page limits bound envelope overhead separately. The counters allow composed reads to enforce one shared budget.

List projection validates the typed envelope and requires a resource version. Omitted item API version/kind inherits
the envelope; present null, empty, or conflicting declarations remain invalid. Continuation tokens are opaque, bounded
control data and are excluded from serialization. Pagination keeps the list resource version fixed, rejects duplicate
UIDs and repeated tokens, and stops at the declared bounds. An expired continuation fails without restarting the list.

Pages are admitted atomically. Malformed, conflicting, or over-budget pages do not discard earlier observations. Results
sort observations by full identity on every outcome. `Collection.Items` counts admitted candidates before association
filtering. GET/LIST methods return candidates without confirming ownership; use the adapter association functions.
Only a valid, complete empty list proves absence in its declared scope.

GET requests can declare `ExpectedUID`. A different UID produces `replaced` with no replacement observation. HTTP 404
produces `deleted` for GET and `unsupported` for LIST; 401/403 produces `denied`; 408/504 produces `timeout`. Other HTTP
failures produce `failed`. Cancellation and transport timeouts remain distinct. Raw response bodies and client errors
are never returned or serialized. Invalid caller configuration returns the bounded `ErrInvalidRequest` error before I/O.

Each primitive call has its own bound. Callers must supply a shared deadline when composing calls themselves, or use the
declared related-read sequence below. These HTTP tests do not qualify real API-server validation, RBAC, or operator
reconciliation. No consumer request scope changes in this slice.

## Discovery and target verification

`Client.Discover` reads exactly `/apis/<group>/<version>` or `/api/<version>` under the endpoint prefix. Its scope names
one resource covered by the selected contract. It makes no resource reads and does not try another group. Projection
checks the `APIResourceList` envelope and group/version, rejects duplicate resource entries, and retains only the selected
resource's kind, namespace scope, and sorted read verbs. Missing or malformed verbs remain unknown or invalid. Mutation
verbs and unrelated metadata are excluded. A complete document can record that the selected resource is absent; this is
not a complete resource LIST and cannot prove object absence.

`adapter.Advertises` checks matching kind, namespaced scope, and a reported read verb. Advertised verbs do not establish
RBAC permission. Discovery does not select omission semantics, change contracts, or qualify an operator integration.

`evidence.NewTargetVerification` also accepts complete unfiltered namespace LISTs, preserving list-only observation
credentials. Partial or filtered lists cannot establish target stability. See [list verification](diagnostics.md#verification-with-list-only-credentials).

`Client.VerifyTarget` accepts a normalized cluster and a matching named GET request with `ExpectedUID`. It performs one
read without discovery. Verification distinguishes matching positive metadata generations, changed generations,
insufficient generation evidence, deletion, replacement, and failed reads. Missing, invalid, zero, or negative generations
cannot verify stability. A matching UID and generation do not establish an atomic snapshot or current health.

`Client.Related` accepts a selected cluster and a `RelatedPlan`. The caller declares optional StatefulSet, Pod, and restore
LIST scopes plus the final pinned target GET. Every scope must match the target's origin and namespace; restore requests
also use its contract. All requests are validated before I/O. Nil scopes cause no reads. The helper collects only declared
scopes, computes UID associations, and performs final verification. It never discovers another group or adds namespace,
Pod, or other optional reads. Consumers retain kubeconfig selection, enrollment, scheduling, and authorization.

Plan limits bound requests, candidates, body bytes, observation bytes, and elapsed time across all scopes and final
verification. Per-request limits can reduce those bounds. One request is reserved for verification. If other budgets or
the deadline prevent verification, its read remains `not_collected`; the batch cannot be complete. Unrequested scopes are
nil; requested but skipped scopes retain `not_collected`. Candidate limits apply before association filtering and exclude
the final target GET. Retention counts the final GET projection conservatively even though verification returns only its
reference and generation. Association and result-envelope overhead is bounded by candidate counts.

Independent observations survive failed reads, deletion, and replacement under the original selected identity. A complete
batch requires complete requested reads, resolved associations, and verified target stability. Candidate, unconfirmed,
or conflicting associations make the batch partial. Foreign candidates stay separate from confirmed ownership. Diagnostics
must retain per-scope outcomes and verification state; incomplete evidence cannot describe a replacement as its predecessor.

The milestone 1 collection fixtures now call production readers through a synthetic transport. Byte units are scaled into
padded response bodies; elapsed deadline cases become typed transport deadline errors. Separate HTTP tests exercise real
context deadlines. Negative byte declarations become malformed wire data. The old admission/verification model is removed.

## Diagnostic evaluation

`diagnose.Analyze` interprets normalized cluster captures with explicit policy and reference time. It implements stable
finding identities, per-rule coverage, and fifteen supported rules. It imports only
evidence and standard-library code. The [evaluation guide](diagnostics.md) records inputs, default policy, versioning,
freshness, verification, and serialized replay. Native recovery and remote replication remain reserved. They cannot be selected silently. Findings can coexist with insufficient coverage,
and empty findings do not establish health.

## Evidence and replay

Normalized cluster/restore records use schema version `2`. Operational fields and object-presence states change the draft
version `1` serialization; validation rejects older records. Neither consumer persists library records yet. Evidence schema,
adapter contract identity, and rule-set versions remain separate. Workload record types also use schema version
`2`; they do not change existing cluster/restore record shapes.

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

Value, condition, identity-validation, freshness, association/selection, workload ownership, collection, finding identity,
coverage, and representative replay fixtures call production code. Field-focused fixtures receive synthetic metadata in
test setup; production always requires identity. Replay fixtures also supply a reported zero backup-failure count so they
isolate the age rule without assuming an omitted counter is zero. Draft fixture envelopes remain test input mappings.
Expected results change only for documented source or reference-model corrections. Container termination-code fixtures
now call the public Pod projector; provider-text canaries check the full production cluster serialization. Remaining test-only
models cover consumer mappings/history, optional local text, and unsupported remote-index specifications. They are not
duplicate supported production projections or diagnostic rules.

Run `go run ./examples/cluster` for a synthetic projection. Current acknowledgement and an implied zero ready count are
resource evidence, not a health check. Run `go run ./examples/diagnose` for deterministic synthetic analysis.
These examples and fixture tests do not qualify an API-server or operator integration. The
[compatibility matrix](compatibility.md) records bounded consumer integration evidence separately. The migrated CLI uses
the production projections, associations, collectors, and rules; [CLI qualification](cli-qualification.md) records its
local checks and remaining integration limits.
