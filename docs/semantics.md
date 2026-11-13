# Evidence semantics

This document defines what normalized evidence means and how the library produces it. Diagnosis depends on these rules.

## Release contract

The client release defines the typed evidence model and diagnostic behavior. Evidence records, captures, policies, and
reports have no separate schema or rule-set versions. Rule IDs identify diagnostics across evaluations.

Adapters select the public API fields and omission semantics at collection boundaries. Their identifiers do not travel
through normalized evidence or evaluation policy. Consumers that store evidence own their enclosing format and reject
unsupported formats before analysis. The client provides no readers or migrations for earlier serialized models.

## Value states

`Integer`, `Boolean`, `Text`, and `Timestamp` record a state with an optional value:

| State | Meaning | Value |
| --- | --- | --- |
| `unknown` | Not observed: the field or an ancestor was absent | None |
| `reported` | The producer reported a valid value | Present |
| `implied` | A contract's omission rule supplies the value | Zero or `false` only |
| `invalid` | The producer reported a malformed value | None; the raw value is never retained |

The zero `State` is invalid. Code that builds evidence must declare `unknown` explicitly. `Conditions`, `Containers`,
`Owners`, `Ordinals`, and `ReadVerbs` use the same states. A reported empty list is not the same as an unknown list.

Optional objects use `ObjectState`:

| State | Meaning |
| --- | --- |
| `unknown` | An ancestor was not observed |
| `absent` | A valid parent omitted the object |
| `null` | The producer reported an explicit `null` |
| `reported` | A valid object; its child fields carry their own states |
| `invalid` | The producer reported a non-object value |

Children of an object that is not `reported` must be `unknown`. Absent or null objects imply no counter, boolean, health,
or operation result.

## Projection contracts

Each projection names one contract. A contract restricts the accepted API version and kinds. Discovery or a matching API
version alone never selects one.

| Contract | Kinds | Omission rules |
| --- | --- | --- |
| `openbao.org/v1alpha1` (`OpenBaoV1Alpha1`) | `OpenBaoCluster`, `OpenBaoRestore` | Yes; also projects restore UID bindings |
| `openbao.org/v1alpha1/unqualified` (`OpenBaoV1Alpha1Unqualified`) | `OpenBaoCluster`, `OpenBaoRestore` | None; restore UID bindings remain unknown |
| `kubebao.org/v1alpha1` (`KubeBaoV1Alpha1`) | `OpenBaoCluster`, `OpenBaoRestore` | Yes; also projects restore UID bindings |
| `kubebao.org/v1alpha1/unqualified` (`KubeBaoV1Alpha1Unqualified`) | `OpenBaoCluster`, `OpenBaoRestore` | None; restore UID bindings remain unknown |
| `apps/v1` (`KubernetesAppsV1`) | `StatefulSet` | Yes |
| `v1` (`KubernetesCoreV1`) | `Pod` | None |

`OpenBaoV1Alpha1` follows operator commit `9671c30623d609f948cf073e167f376de4ea9a85`.
`KubeBaoV1Alpha1` follows native operator candidate `22894cdc0a4aa5c442a07f2bf9902fe28ce02ea1`.
The Kubernetes contracts follow the
public API declarations at `v0.37.2` and workload metadata at operator revision `9671c306`. Their `SourceRevision` identifies
that operator source. `adapter.Describe` returns each contract's kinds and capabilities. It does not perform discovery,
check RBAC, or qualify an operator.

The native API contract retains the operator's `openbao.org/*` workload labels and annotations. API-group selection
does not rename metadata. Identity includes the API group and UID. A migration annotation does not establish that a
legacy object and a native object are the same target. Restore association requires a supported, matching API group and
version and confirmed UID bindings; names and migration metadata cannot join resources across groups. Unqualified
adapters leave the cluster's restore-origin UID and the request's target UIDs unknown.

The operator contract includes `metadata.deletionTimestamp` on clusters. An omitted timestamp remains `unknown`;
reported and invalid timestamps prevent control preparation. Validation checks the current typed shape after replay.

### Workload pools and revisions

StatefulSet pool and revision facts come from `spec.selector.matchLabels` and `spec.template.metadata.labels`.
The adapter records these fields separately as `SelectorPool`, `SelectorRevision`, `TemplatePool`, and
`TemplateRevision`. StatefulSet top-level labels do not supply these facts. Pod pool and revision facts come from
`metadata.labels`. Only `openbao.org/workload-pool` and `openbao.org/revision` are approved for grouping.

Pool values are `voter` or `read-replica`. Revisions are nonempty Kubernetes label values of at most 63 bytes, with
alphanumeric first and last characters and alphanumeric, `-`, `_`, or `.` characters inside. Missing keys remain
`unknown`; present empty, null, malformed, or unsupported values become `invalid`. Malformed or null label maps
invalidate their projected fields. Unapproved labels are excluded. Validation applies the same constraints after replay.
Pool values describe declared workload groups. They do not establish live Raft membership.

These fields do not prove ownership. Maintenance assessment first confirms the UID association, then checks agreement
between selector, template, and Pod fields. A Pod's matching name or labels cannot establish membership.

The cluster projection also records `status.blueGreen.blueRevision` and `greenRevision` with the same revision syntax.
Under the qualified operator contract, an `Idle` blue/green object with an omitted Blue revision selects the original
unrevisioned voter pool. This interpretation belongs to assessment; the projected value remains `unknown`.

### Omission rules

An omitted primitive stays `unknown` unless one of these rules applies:

- `status.readyReplicas` (cluster and StatefulSet) is implied `0` only when `metadata.generation` and
  `status.observedGeneration` are equal positive integers and `spec.replicas` is reported. Missing `spec.replicas` stays
  unknown; projection does not apply API-server defaults.
- `spec.paused` (cluster) is implied `false` when the `spec` object is present and valid. A missing `spec` stays unknown.

These rules follow non-pointer `omitempty` fields in the pinned sources. Failure counts are never implied as zero, and
history-conflict flags are never implied as false.

### Projection input

`ProjectCluster`, `ProjectRestore`, `ProjectStatefulSet`, `ProjectPod`, their `List` variants, and `ProjectDiscovery`
accept JSON bytes and `ProjectionOptions`: origin, collection time, contract, and limits. They perform no I/O and read no
clock.

The decoder rejects these inputs:

- Duplicate object keys, invalid UTF-8, trailing documents, and nesting deeper than 64 levels.
- A mismatched `apiVersion` or `kind`, and missing or invalid identity.

Numbers are decoded as exact integers, never as floating point. List items without `apiVersion` or `kind` inherit them
from the list envelope. A null, empty, or conflicting declaration rejects the list.

`DefaultProjectionLimits` allows 8 MiB per document, 256 conditions per set, and 256 entries per owner, ordinal, container,
or verb set. Limits must be positive. A document over the byte or depth limit is rejected. A set over its limit becomes
`invalid`, and the other fields are kept.

Errors are `*adapter.ProjectionError` with a bounded `Code`: `invalid_input`, `projection_limit`, `unsupported_contract`,
or `resource_mismatch`. Errors never contain parser messages, source values, or response bodies.

### Field validation

- Explicit null primitives, wrong types, fractional or negative counts, and out-of-range integers are `invalid`.
- Counts are `int32` ranges. Container exit codes keep the full signed `int32` range. Generations keep exact `int64` values.
- Text is non-empty, at most 253 bytes, and contains no control or replacement characters. Structured codes such as
  reasons and phases must also match `^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`. Enumerated fields accept only their declared values.
- Versions follow the operator's public `spec.version` semantic-version pattern. Snapshot digests must match `sha256:<64 hex>`.
- Condition types must be unique and statuses must be `True`, `False`, or `Unknown`. An invalid type, status, or observed
  generation, or a duplicate type, invalidates the whole set. An invalid reason or transition time is invalid only at
  field level. Conditions are sorted by type.
- An absent or null annotation map has no keys. A malformed map, or a null value for a present key, is invalid.
- Malformed ancestors invalidate their children. Independent valid fields are kept.

### Restore retention evidence

`ProjectRestore` retains reported `spec.targetLifecycle`, `spec.cleanupAfterSeconds`, and
`status.execution.terminalResult`. Lifecycle accepts `Retain` or `Disposable`; retention accepts integers from `0` to
`86400`; terminal result accepts `Succeeded` or `Failed`. Missing values remain unknown. Projection does not default an
omitted retention value or infer application, cleanup, or recovery from these fields.

### Excluded data

Projection never retains provider messages, resource specs other than approved fields, arbitrary annotations, environment
variables, raw errors, or Secret data. Condition and container records keep reason codes, not messages.

## Identity and references

The caller supplies a stable origin that identifies the Kubernetes API server. A kubeconfig context name alone is not a
portable origin. Resource identity is `(origin, group, kind, namespace, name, UID)`. An empty group is the core group.
Each component is at most 253 bytes and contains no control or replacement characters. Invalid components are rejected,
not sanitized.

A `Reference` adds API version, resource version, read time (`CollectedAt`), and an optional field path. These values are
not part of identity. Equal UIDs in different origins are different resources.

## Generations

`evidence.CompareGeneration` compares a generation with its acknowledgement:

| Result | Condition |
| --- | --- |
| `unknown` | Either value is missing or zero |
| `stale` | Acknowledgement is lower than the generation |
| `current` | Both are equal and positive |
| `inconsistent` | Either value is negative, or acknowledgement is higher than the generation |

Compare resource status and each condition separately. A current resource does not make a stale condition current.
`current` means the producer acknowledged the generation, not that the observation is recent.

## Time

The library keeps four kinds of time separate:

- **Collection interval**: the start and end of one read attempt. It is not an atomic snapshot.
- **Read time**: when one resource was read. It falls within its collection interval.
- **Producer time**: a timestamp the operator or Kubernetes reported, such as a backup completion or a condition
  transition. A producer time after its read time is inconsistent. A transition time is not a heartbeat.
- **Reference time**: the analysis time the caller supplies. It is at or after collection completion.

`evidence.AssessFreshness` compares a producer time with the reference time and a maximum age, with no clock-skew
tolerance. An age equal to the limit is within the limit.

## Associations

An `Association` compares a subject with a target and records one state:

| State | Meaning |
| --- | --- |
| `confirmed` | UID evidence binds the subject to the target. A `Basis` records which field. |
| `candidate` | Names match, but no UID evidence confirms the binding. |
| `unconfirmed` | Not enough evidence either way. |
| `foreign` | Evidence binds the subject elsewhere, or it is outside the target's origin or namespace. |
| `conflict` | The evidence both matches and contradicts the target. |

Names, labels, and creation times identify candidates only. They never confirm ownership.

### Workloads

- A StatefulSet is confirmed when it has exactly one controller owner reference, and that reference matches the cluster's
  group, kind, name, and UID. API version differences within the group do not matter.
- A Pod is confirmed when its controller reference matches a StatefulSet that is itself confirmed to the cluster.
- The `openbao.org/owner-uid` annotation refers to the cluster UID on both kinds. A conflicting or malformed value blocks
  confirmation. A matching value alone confirms nothing.
- A Pod is excluded from StatefulSet membership when it has exactly one valid controller reference to another kind or API
  group, such as a backup Job. This does not prove that the Job belongs to another cluster.
- A Pod whose consistent controller chain leads to a StatefulSet owned by another cluster is `foreign`.
- Missing, invalid, non-controller, or multiple controller references leave a Pod unconfirmed.
- All edges stay within one origin and namespace. A replaced parent invalidates the old chain.

### Restores

`evidence.AssociateRestore` checks every supplied target name and UID before confirming:

1. A request in another origin, namespace, or API group is `foreign`.
2. A request is unconfirmed when its API version differs from the target's or the API is unsupported.
3. A malformed binding object or field leaves the request unconfirmed.
4. Any mismatch with the target name or UID makes the request `foreign`, or `conflict` if another field also matches.
5. Otherwise the request is confirmed by `status.execution.targetUID`, then by `status.target.uid`, then by the cluster's
   `openbao.org/restore-origin` annotation naming the request UID.
6. A matching name alone gives `candidate`.

An `openbao.org/restore-hold` value or operation ID never proves ownership. `diagnose.LatestRestore` selects a confirmed
request for display: the newest valid creation time at or before its read time, then UID ascending. It rejects duplicate
identities. Selection does not prove causality or success.

## Collection

### Client and transport

`collect.NewClient` takes an API-server endpoint and a caller-configured `http.RoundTripper`. The caller owns TLS,
authentication, credential renewal, proxies, and rate limits. The transport must honor request contexts and must not
follow redirects. The client sends GET requests only. It loads no credentials, follows no redirects, retries nothing, and
never tries another API group. Endpoints with userinfo, query strings, fragments, encoded paths, or traversal segments are
rejected.

### Requests

`Clusters`, `Restores`, `StatefulSets`, `Pods`, and `Discover` each accept a `Request`: an exact `evidence.Scope`, an
adapter contract, collection limits, and projection limits.

- Each method accepts only its own resource and a contract that covers it.
- A named GET requires a namespace and can declare `ExpectedUID`. A different UID gives `replaced` and no observation.
- An all-namespace LIST also requires `AllowAllNamespaces`.
- Label selectors pass through unchanged.
- Discovery reads only `/apis/<group>/<version>` or `/api/<version>`, and keeps only the selected resource's kind,
  namespace scope, and read verbs. Advertised verbs do not prove RBAC access.

Invalid requests fail before any I/O. The error is a `*evidence.ValidationError` that names the failing field, and
`errors.Is(err, collect.ErrInvalidRequest)` matches it.

### Limits

`DefaultLimits` allows 8 requests, 500 candidates, 100 candidates per page, 8 MiB per response, 8 MiB of response bodies in
total, 8 MiB of serialized observations, and 15 seconds per GET or LIST. Every limit must be positive. Page size cannot
exceed the candidate or projection entry limit. A shorter caller deadline takes precedence.

`Result.Bytes` counts body bytes read, including partial or oversized bodies. `RetainedBytes` counts serialized
observations. Callers that compose reads themselves must supply a shared deadline, or use `Related`.

### Outcomes

Each read records one `evidence.CollectionOutcome`. Raw errors and bodies are never returned.

| Outcome | Cause |
| --- | --- |
| `complete` | All pages read and admitted |
| `partial` | Some reads in a sequence did not complete |
| `denied` | HTTP 401 or 403 |
| `deleted` | HTTP 404 on a named GET |
| `unsupported` | HTTP 404 on a LIST or discovery |
| `expired` | HTTP 410, such as an expired continue token. The list is not restarted. |
| `rate_limited` | HTTP 429 |
| `timeout` | HTTP 408 or 504, a transport timeout, or the deadline |
| `cancelled` | The caller cancelled the context |
| `page_limit`, `size_limit` | A request, candidate, or byte limit was reached |
| `pagination_loop` | A continue token repeated |
| `replaced` | The UID differs from `ExpectedUID` |
| `invalid` | The response failed projection or admission |
| `failed` | Any other failure |
| `not_requested`, `not_collected` | No read was requested, or a requested read was skipped |

Denied and unsupported reads are never empty successes. Only a valid, `complete`, empty LIST proves absence within its
declared scope. A named GET or discovery cannot prove absence.

### Pagination and admission

Pagination keeps the list resource version fixed, rejects duplicate UIDs, and stops at the limits. Pages are admitted
whole: a bad page stops the read but keeps the pages before it. `Collection.Items` counts admitted candidates before
association filtering. Results are sorted by identity. GET and LIST methods return candidates; they do not confirm
ownership.

### Target verification

`evidence.NewTargetVerification` compares a selected cluster with a later named GET or a complete, unfiltered namespace
LIST. A LIST lets list-only credentials verify targets. The result is one of these states:

| State | Meaning |
| --- | --- |
| `verified` | Same UID and same positive generation |
| `changed` | Same UID, different generation |
| `insufficient` | A generation is missing or invalid, or the read was not collected |
| `deleted` | The name is absent from a complete read, or a named GET returned 404 |
| `replaced` | The same name has a new UID |
| `failed` | The read did not complete |

A partial or filtered LIST never verifies a target. Verification is not an atomic snapshot. `Client.VerifyTarget`
performs one named GET with `ExpectedUID` and returns this result.

### Related reads

`Client.Related` takes a selected cluster and a `RelatedPlan`. The plan declares optional StatefulSet, Pod, and restore
LISTs and a final verification GET. Every scope must match the target's origin and namespace, and restore reads use the
target's API group and version. Nil scopes cause no reads. Requests are validated before I/O.

Plan limits apply across the whole sequence. One request is reserved for verification. If the budget or deadline runs
out first, verification stays `not_collected` and the sequence cannot be `complete`. Skipped scopes record
`not_collected`.

The result keeps read outcomes, associations, and verification separate. Association ambiguity and changed generations
do not change read outcomes. Observations from a deleted or replaced target are never attributed to its replacement.

## Validation errors

`Validate` methods check normalized values after construction or JSON decoding. Failures return
`*evidence.ValidationError` with a field path such as
`clusters[0].pods.observations[3].metadata.reference.resourceVersion`. `errors.Is` still matches sentinel errors such as
`evidence.ErrInvalidEvidence`. Error text contains field names and indices only, never values.
