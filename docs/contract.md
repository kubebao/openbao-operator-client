# Evidence contract decisions

These decisions define the extraction target. Only generation acknowledgement currently has an executable library
contract. Other decisions require fixtures and expected-result tests before implementation moves from a consumer.

Baseline consumers:

- Console: `9e7b6900f781e8fbac736ca42e6a51f7f807087b`.
- CLI: `adbe0f42fac62bdc6dd2d7995a8aecb36102e9a3`.

The inspected operator source is `9671c30623d609f948cf073e167f376de4ea9a85`. This records source provenance only. Each
adapter must identify the public schema/behavior it implements; the source snapshot is not integration qualification.

| Topic | Canonical decision | Baseline treatment | Executable coverage |
| --- | --- | --- | --- |
| Missing values | Preserve unknown values. Interpret an omitted zero only under a documented adapter contract and its acknowledgement preconditions. Invalid and absent values remain distinct. | Preserve unknowns; correct blanket defaults where necessary. | Pending |
| Generations | Compare resource and condition acknowledgement independently. Missing or zero values are unknown; older acknowledgement is stale; equal positive values are current; future or negative values are inconsistent. | Correct console paths that accept future generations as current. | `evidence/testdata/generations.json` |
| Associations | Include origin scope, public resource identity, and UID. Retain association basis. Conflicting identity fields prevent confirmation. Names, labels, and creation times identify candidates only. | Correct name/time-only console recovery associations. | Pending |
| Text | Exclude provider messages by default before creating serializable evidence. Optional local text uses a separate policy with byte/field bounds. | Preserve console exclusion; make richer CLI projection opt-in. | Pending |
| Findings | Identify each finding by canonical rule, stable subject identity, and a subsubject where needed. Define severity mappings and deterministic ordering. | Replace provisional identities deliberately; map or migrate console history. | Pending |
| Time | Separate collection interval, per-resource collection time, producer observation time, transition time, and analysis reference time. | Preserve distinctions; remove evaluation-time inference from projections. | Pending |
| Scope and coverage | Distinguish excluded rules, requested but uncollected evidence, denied/unsupported reads, insufficient evidence, and evaluated rules. | Preserve consumer scope without imposing CLI workload reads on the console. | Pending |
| Partial collection | Only a successful complete scope establishes absence. Keep per-scope outcomes and supported findings after partial reads. | Correct optional denied console reads that can clear recovery findings. | Pending |
| Process results | The shared contract contains no CLI process exit codes or failure threshold decisions. Container termination codes remain workload evidence. | Move snapshot/report process codes into CLI wrappers. | Pending |
| Replay | The same validated evidence, rule set, policy, and reference time produce the same ordered results. Version evidence, adapter contracts, and rules separately. | Preserve CLI deterministic replay and extend it to the combined rules. | Pending |

## Generation acknowledgement

`evidence.CompareGeneration` implements the generation decision. Call it once for resource status and once for each
condition. Current resource acknowledgement does not make a stale or missing condition current. Equality does not prove
recent reconciliation or application health. A negative value remains inconsistent even if the other value is absent.

Synthetic fixtures cover missing/zero values, stale/current/future acknowledgement, negative values, and resource/condition
disagreement. They do not test Kubernetes defaulting, admission, or status-subresource behavior.

## Required baseline sequences

Before extraction, add executable expected results for:

- A confirmed recovery failure followed by an optional recovery read denial; retain the unresolved finding as last known.
- A deleted/recreated target with the same name and a new UID; do not inherit ownership or request correlation.
- Conflicting restore target and execution UIDs; do not choose the matching field and ignore the conflict.
- A complete empty list versus a partial or denied list; only the first establishes absence in its scope.
- Reordered input resources, conditions, and equal-time requests; produce stable associations, selections, and findings.
- Multiple subjects for one rule, repeated console collection, and a mapped identity change; preserve documented history behavior.
- Provider-message canaries, malformed fields, duplicate conditions, and optional bounded local text.
- Serialized evidence evaluated with a fixed reference time and declared rule selection.
