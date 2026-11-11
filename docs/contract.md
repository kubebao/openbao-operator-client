# Evidence contract decisions

Milestone 1 defines the extraction target through synthetic fixtures, executable reference specifications, and tests of
the recorded consumer source. Milestone 3 now implements cluster, operational, and workload evidence/projection;
UID associations; identity validation; times; and collection outcomes. Matching fixtures call production code. Remaining
specifications are test-only. Bounded discovery, GET/LIST, and declared related-resource collection with final target
verification are implemented. The diagnostic core implements twelve cluster rules; other rules remain pending or
reserved. See [extraction status](extraction.md) and [evaluation behavior](diagnostics.md).

Source baselines:

- Console: `9e7b6900f781e8fbac736ca42e6a51f7f807087b`.
- CLI: `adbe0f42fac62bdc6dd2d7995a8aecb36102e9a3`.
- Operator API and behavior inspected: `9671c30623d609f948cf073e167f376de4ea9a85`.

The fixture contract `openbao.org/v1alpha1@9671c306` identifies that source snapshot. It is not an operator release
qualification. Discovery alone cannot select zero-value or ownership semantics for another schema.

## Decisions and executable coverage

Paths are relative to [the reference specifications](../internal/contracttest). JSON files contain inputs and expected
results. [Consumer baselines](baselines.md) identify preserved behavior and intentional corrections separately.

| Topic | Decision | Expected-result tests |
| --- | --- | --- |
| Missing values | Distinguish absent, reported, implied-zero, and invalid values. Infer omitted zero/false only under the preconditions below. | `testdata/values.json`; `TestMissingValues` |
| Generations | Compare resource and condition acknowledgement separately. Missing/zero is unknown; older is stale; equal positive values are current; future/negative is inconsistent. | [`generations.json`](../evidence/testdata/generations.json); `testdata/conditions.json` |
| Associations | Include origin and resource identity. Retain the confirmation basis. Conflicts prevent confirmation. Names, labels, and creation times identify candidates. | `testdata/associations.json`, `ownership.json`, `selection.json` |
| Text | Exclude provider messages before serialization. Optional local text uses a separate bounded sidecar. | `testdata/projection.json`, `text.json`; `TestLocalTextPolicy` |
| Findings | Use canonical rules and stable subject/subsubject tuples. Map severities in wrappers and order results deterministically. | `testdata/rules.json`, `findings.json`, `history.json`, `replay.json` |
| Time | Separate collection interval, resource read time, producer time, transition time, and analysis reference time. | `testdata/time.json`, `replay.json` |
| Scope and coverage | Separate rule selection, requested scope, applicability, collection outcome, and evaluation coverage. | `testdata/coverage.json`, `replay.json` |
| Partial reads | Only a valid, complete scope proves absence. Retain supported findings after partial reads. | `testdata/collection.json`, `coverage.json`, `history.json` |
| Process results | Keep process codes and failure thresholds in wrappers. Container termination codes remain workload facts. | `testdata/projection.json`; `TestSafeProjection` |
| Replay | Require known evidence, adapter, and rule versions. Supply policy, reference time, and selected rules. Reject duplicate subjects. | `testdata/replay.json`; `TestSerializedReplay` |

Replay exercises representative readiness and backup-age rules. The existing rule inventory runs in archived consumer
suites. Milestone 3 replaces reference-model calls with production code while retaining fixture inputs and expected results.

## Missing and malformed fields

The initial source contract permits two omission interpretations:

- Absent `status.readyReplicas` means zero only when resource and observed generations are equal positive integers and
  valid `spec.replicas` is present. Record implied zero separately from a reported count.
- Absent `spec.paused` in a valid spec object means false under the known contract. Record implied false. Spec values need
  no acknowledgement. A missing spec object remains unknown.

These follow the non-pointer `omitempty` fields in operator `api/v1alpha1/openbaocluster_status_types.go` and
`openbaocluster_types.go`. Other absent primitive values remain unknown until their adapter contract defines omission
semantics. Optional objects record whether the field is absent within an observed parent; this does not imply a counter,
boolean, health, or operation result. Do not default missing failure counts to zero or history-conflict flags to false.

The `kubernetes/apps/v1@v0.37.2` contract applies the same readiness preconditions to StatefulSets, whose public
`apps/v1.StatefulSetStatus.ReadyReplicas` is a non-pointer `omitempty` count. Missing `spec.replicas` remains unknown;
projection does not perform API-server defaulting. The separate `kubernetes/core/v1@v0.37.2` contract covers Pods.

Explicit null primitive values, wrong types, fractional/negative counts, and out-of-range integers are invalid. Optional
objects preserve null, absence, unknown ancestors, and malformed values separately. Null objects retain no child facts and
imply no health or operation result. Malformed ancestors invalidate their fields. Preserve independent valid observations.
An absent or null metadata annotation map has no reported keys. A malformed map or a null value for a present key is invalid.
Keep generations exact above the floating-point range.
Raft indexes must be canonical unsigned decimal strings within `uint64`; never convert them through floating point.

Condition types are unique and nonempty. Status is `True`, `False`, or `Unknown`. Invalid types, statuses, observed
generations, or duplicate types invalidate the condition set without discarding independent scalars. Invalid optional
reasons and transition times retain field-level invalid states. Sort valid conditions by type. Missing conditions
remain unknown; a valid empty list records an empty set. Unknown condition types acquire no interpretation automatically.

## Generations and time

Call `evidence.CompareGeneration` for resource status and each condition. Current resource acknowledgement does not make a
stale condition current. Negative values are inconsistent even when the other value is absent. Equality is not freshness.

Current readiness requires current resource acknowledgement. Preserve stale reported counts but mark current readiness
coverage insufficient. Availability requires current resource and `Available` condition acknowledgement. A sealed finding
requires current `OpenBaoSealed` condition acknowledgement. Historical operation failures remain reported facts.

Record the collection interval and each resource's read time. Read times fall within the ordered interval. Analysis
reference time is at or after completion. Parse producer times independently; times after their read are inconsistent.
Missing producer time means unknown freshness. Transition time is not a heartbeat. The reference policy permits no clock
skew; any future tolerance must be declared. Projections do not infer pending/running states from request age.

Age comparisons use the supplied reference time. Backup age must exceed its limit to be overdue; equality is within the
limit. Startup grace suppresses readiness findings until its duration has elapsed. Changing policy or reference time can
change findings without changing evidence.

## Identity, association, and selection

The caller supplies a stable Kubernetes origin identifier. A kubeconfig context name alone is not portable cluster
identity. Resource identity is `(origin, group, kind, namespace, name, UID)`. API version, resource version, generation,
collection time, severity, and text do not form stable identity. References retain API version, resource version, read time,
and field selector separately. Reject missing or control-character identity components; do not sanitize them into collisions.

Restore association checks every supplied target name and target/execution UID. Matching fields never override conflicts.
Confirmation requires a known contract and a matching target/execution UID, or the selected target's `restore-origin`
annotation bound to the request UID. Retain the exact basis. A `restore-hold` value can be an operation ID: a matching
operation ID alone does not prove ownership. Correlate it only after confirming the request-to-target association.

StatefulSets need one consistent controller owner reference: group, kind, name, and UID must match. The controller flag
must be true. API version differences within the same group do not change identity. Pods need a controller reference to a
confirmed StatefulSet. `openbao.org/owner-uid` refers to the cluster UID on both resources. Conflicting or malformed owner
annotations prevent confirmation; an annotation alone is insufficient. Every edge stays within origin and namespace.
Replacing either parent invalidates the old ownership chain.

Names and creation times retain candidates for display, never confirmed recovery findings. Among confirmed requests with
valid creation times at or before their read time, display newest first and break equal-time ties by UID ascending.
Reject duplicate request identities. This order does not prove causality or acceptance.
Source inspection found no public `OpenBaoRecovery` API in the pinned operator. This contract cannot confirm
that kind even when synthetic fields match a UID. Native association fixtures were corrected to require unconfirmed
results. See [source corrections](extraction.md#source-corrections).

## Findings and severity

The [rule catalog](rules.md) maps all console rules, CLI doctor rules, and CLI status attention. Finding identity is the
ordered tuple `(rule, origin, group, kind, namespace, name, UID, subsubject)`. Tests encode it as a JSON string array;
production uses that string-array key through `diagnose.Finding.Key`. Use condition type, restriction field, controller domain, or container
kind/name for multiple concerns on one resource. Use an empty subsubject for one concern per resource. Exclude severity,
text, timestamps, policy values, and resource version. Sort by severity descending, then the tuple lexically.

| Library severity | Console severity | CLI severity |
| --- | --- | --- |
| `info` | `info` | `info` |
| `warning` | `high` | `warning` |
| `error` | `high` | `error` |
| `critical` | `critical` | `error` |

Zero ready replicas, sealing, and history conflicts remain critical. Other console high findings become warning unless a
matching CLI error interpretation applies. Failed Pods and unknown/failed held restores remain error. Restrictions are
informational except break glass, which remains warning. Findings can coexist with insufficient coverage.

## Collection scope and history

Coverage distinguishes `excluded`, `not_requested`, `not_applicable`, `insufficient`, and `evaluated`. Each entry retains
full resource identity, including origin; UIDs alone do not identify subjects across Kubernetes clusters. Preserve scope
outcomes: denied, unsupported, timeout, cancellation, malformed data, pagination/size limits, deletion, replacement.
Denied or unsupported reads are insufficient, never successful empty lists. Only known applicability justifies
`not_applicable`. Evaluated rules can have no findings. Empty findings do not establish health.

Bound pages, candidates, retained bytes, and total duration. Reject zero limits, repeated continuation tokens, duplicate
UIDs, invalid sizes, and unclassified errors. Retain admitted observations after later page failures with incomplete
coverage. Recheck target UID; observations from an old UID cannot describe its replacement.

Keep consumer scope: `status` reads the named cluster and discovery; `doctor` requests additional bounded reads; watch
stays pinned to group and UID. Console observation remains read-only without new Pod reads or namespace discovery.
Archived request tests record the baseline. Migrated command/collector tests must verify those boundaries again.

The console owns first-seen times, persistence, and history. Repeated unresolved findings retain first-seen time. Missing
scopes retain findings as last known without advancing their last confirmed evaluation time. Complete valid evaluation
can resolve findings. A new episode after resolution gets a new first-seen time. Replacements inherit no findings or
request correlation. Reviewed one-to-one legacy mappings can retain history; ambiguous mappings require a versioned
migration or development reset during milestone 4.

## Text and verification boundaries

Default projection excludes provider messages, resource specs, arbitrary annotations, raw errors, and Secret contents.
Regex redaction does not make arbitrary text safe for storage. The local sidecar permits `condition.message`,
`container.waiting.message`, and `restore.message` in that order: 256 UTF-8 bytes per field and 512 bytes per resource. Remove
control characters and truncate on rune boundaries. This sidecar stays outside serialized evidence and bundles. It can
contain sensitive text and gives no redaction guarantee. Consumers opt in before projection and keep it local. Production
support remains pending.

`make check` runs self-contained specifications. [Baseline verification](baselines.md) runs archived source tests.
Synthetic tests do not qualify Kubernetes defaulting, admission, status writes, real RBAC, reconciliation, or lifecycle
operations. [Compatibility evidence](compatibility.md) records these limits.
