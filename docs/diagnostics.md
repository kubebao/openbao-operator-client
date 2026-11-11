# Diagnostic evaluation

`diagnose.Analyze` receives a typed `Capture` and `Policy`. It reads no clock, network, environment, credentials, or product
state. Identical evidence and policy produce the same report when subject, selected-rule, or reference order changes.
Empty findings do not establish health or complete coverage.

Rule set `7` implements fifteen rules: availability, sealing, replica readiness, recognized adverse conditions, generation
acknowledgement, controller errors, restrictions, three backup concerns, recorded rolling-upgrade failure, restore tests,
StatefulSet readiness, Pod concerns, and snapshot-restore progress/recovery concerns.
`SupportedRules` returns the executable inventory. Earlier rule sets are rejected rather than silently extended. Other
canonical IDs remain in the [catalog](rules.md), but selecting one returns `ErrUnavailableRule`. Unknown IDs return
`ErrUnknownRule`. Reserved native-recovery and
remote-replication IDs cannot produce findings. Expanding the executable inventory or changing rule semantics requires a
new rule-set version. Report layout version `1`, evidence schema `2`, and projection contracts are separate versions.

## Inputs and policy

Each `ClusterInput` declares a full subject identity, projection contract, collection outcome, optional normalized cluster,
and optional target verification. This preserves insufficient coverage for known subjects whose reads failed. Complete
reads must contain the observation's read time and a positive candidate count. Subject, scope, contract, identity, API
version, and verification must agree. Duplicate subjects are rejected; identical UIDs in different Kubernetes origins remain
different subjects. Invalid normalized shapes fail analysis without returning partial findings or raw input values.

The caller supplies an approved projection-contract allowlist after selecting adapters. The analyzer checks membership;
it does not implement adapters, infer a contract from API discovery, or qualify an operator. An unknown evidence or rule-set
version is rejected. Contracts outside the caller's allowlist are rejected.

`DefaultPolicy` requires a reference time and contract allowlist. It selects the supported rules and sets a 24-hour backup
age limit, 1-minute startup grace, 5-minute evidence age limit, and 5,000-subject limit across selected clusters and all
supplied workload and restore candidates. Durations and subject limits must
be positive, except startup grace can be zero. Rules and contracts must be unique. A reference time before collection
completion is invalid. These are evaluation parameters, not process exit thresholds.

`Capture` contains normalized value types, not raw Kubernetes resources. `Analyze` validates typed inputs and versions;
consumers own JSON decoding and byte limits. Serialized replay tests use trusted fixtures. They do not implement or qualify
an untrusted bundle parser. Bundle handling remains a separate future package.

### Related resource inputs

`ClusterInput.StatefulSets`, `Pods`, and `Restores` are optional `ResourceInput` values. Each retains its projection contract,
declared
LIST outcome, and every admitted candidate before UID filtering. Nil means no scope was requested. Complete inputs must
retain all candidates, including foreign resources, and a non-nil empty slice for a valid empty list. Scope, origin,
namespace, resource, version, contract, read times, and candidate counts must agree. Duplicate UIDs or names are rejected.
The policy allowlist must include each supplied contract, including an empty or failed read's contract.

Restore reads use the selected cluster's group, version, and projection contract. Related reads follow the selected cluster
read and precede final verification when verification was attempted. Related-resource
findings require successful target verification with the original source reference and generation. Verification does not
establish an atomic snapshot. A missing, changed, failed, deleted, or replaced verification prevents current findings.

The analyzer recomputes associations from normalized owner references and cluster-owner annotations through the same pure
`evidence` implementation used by adapter entry points. Caller-supplied association assertions are not analysis inputs.
Only confirmed chains can produce workload findings. Unconfirmed/conflicting candidates make coverage insufficient;
foreign StatefulSets do not become selected-cluster workload facts. A Pod whose controller edge and reserved metadata
consistently bind it to a foreign StatefulSet is excluded from selected-cluster population and coverage. A conflicting
Pod annotation remains unconfirmed. Parent association assertions are checked against the StatefulSet metadata.

Each workload rule has one coverage entry under the selected cluster, with its primary LIST outcome. Findings identify
the affected StatefulSet or Pod. Pod coverage also depends on the StatefulSet read. Capture inputs retain both scopes;
the primary outcome alone does not establish complete coverage. Unrequested workload scopes remain `not_requested` even
when the selected cluster read fails.

### Restore evaluation policy

`RestoreUIDBindingContracts` is a caller-selected subset of `ApprovedContracts`. Select a contract only when
`adapter.Describe` declares `CapabilityRestoreUIDBinding`. The inspected operator snapshot declares it; unqualified field
projection and discovery do not. Empty or duplicate declarations, or bindings outside `ApprovedContracts`, cannot qualify
restore associations. A missing binding declaration leaves requested restore coverage insufficient. Adapter entry points
and diagnosis call the same pure UID-binding implementation. The analyzer does not accept caller assertion of ownership.

`DefaultPolicy` enables `ReportHistoricalRestores` to preserve the console's broader recorded-outcome coverage. Set it to
false when preserving CLI doctor's exclusion of unheld historical outcomes during migration. Both choices use the same
rule and retain the same evidence; the policy is serialized in the report. This choice does not change held concerns.

## Findings and coverage

A finding key is the JSON string array `(rule, origin, group, kind, namespace, name, UID, subsubject)`. Severity, text,
timestamps, policy, and resource versions do not change identity. Findings sort by severity descending, then identity.
References sort by full read identity, field, API version, resource version, and read time. Canonicalization copies reference
data and rejects duplicate finding identities. Summaries are library-authored text; provider messages are excluded.
Console/CLI severity mappings, rendering, first-seen times, history, and process results remain consumer responsibilities.

Coverage distinguishes `excluded`, `not_requested`, `not_applicable`, `insufficient`, and `evaluated`. Each entry records
the rule, full subject, bounded reason, and collection outcome. `ResolveCoverage` combines selection, requested scope,
known applicability, collection validity, and evidence validity. Only a complete valid empty LIST confirms scoped absence;
GET and discovery do not.

Fresh observations admitted before a later collection failure can support findings while coverage remains insufficient.
Old observations outside the current attempt or beyond the evidence age limit cannot produce current findings. A deleted
or replaced subject cannot produce current findings. Supplied target verification must match the source read, including
resource version and generation; changed, insufficient, or failed verification prevents current findings. The consumer
can retain earlier findings as last known without treating them as newly confirmed.

## Implemented rules

Availability requires positive current resource and individual `Available` condition acknowledgement. Current
`Available=False` produces a warning. Unconfirmed resource/condition generations or an `Unknown` status yield insufficient
coverage and no availability finding.

Sealing requires its own current `OpenBaoSealed` condition and known pause state. `OpenBaoSealed=True` is critical when
reconciliation is not paused. Pause suppresses this finding, preserving console behavior. A condition can be current while
the resource's overall acknowledgement is stale; the two observations remain separate.

Replica readiness requires positive current resource acknowledgement, known desired/ready counts and pause state, and a
valid creation time no later than its read. Pause, startup grace, zero desired replicas, or enough ready replicas suppress
the finding. Zero ready replicas is critical; a positive shortfall is warning. Grace equality is outside the grace period.
Condition acknowledgement remains separate; this rule does not establish operator availability or client transactions.

The generic condition rule interprets only the recognized condition names and polarities in the CLI baseline. Adverse
negative conditions such as `Degraded=True` and positive conditions such as `TLSReady=False` produce warnings. Each
condition needs its own current acknowledgement and uses its type as a subsubject. Unknown names are ignored; unknown
status and unconfirmed generations are insufficient. Availability and sealing use their specialized rules without generic
duplicates. Missing transition times do not block a generation-confirmed status report. A malformed or future transition
time makes coverage insufficient while retaining an independently confirmed status finding. Transition time is not a heartbeat.

The generation rule distinguishes stale from inconsistent acknowledgement with separate warning text. Missing/zero
acknowledgement is insufficient; it is not a mismatch finding.

The restriction rule keeps pause, maintenance, restore hold, and break glass independent. Pause, maintenance, and a reported
hold are informational; active break glass is warning. The exact source field identifies each finding. Maintenance
propagation is not verified, a hold proves no restore ownership, and these findings authorize no operations. Unknown or
invalid reported flags make coverage insufficient while preserving independent known restrictions. Absent/null optional
objects and unreported hold annotations produce no restriction report; this does not infer a false producer value or health.

Controller errors use `workload` or `adminOps` as subsubjects. A reported bounded reason produces a warning about a recorded
error, not a claim of active failure. Missing or malformed reported reasons are insufficient. Invalid/future error times
make coverage insufficient while retaining the independently reported error. Absent/null error objects produce no report.
Provider reasons, messages, hold values, and break-glass instructions are excluded from diagnostic text.

Backup age requires observed backup configuration, reported backup status, a known zero failure count, and a success time
no later than its read. Known absent configuration is not applicable; missing/invalid configuration, failure counts, or
timestamps are insufficient. A positive failure count suppresses the age finding to preserve backup-failure precedence.
Selecting only backup age does not evaluate failures. Completion age must exceed the limit to warn; equality is within the
limit. Completion age is not measured recovery point objective (RPO).

The backup-failure rule warns on a reported positive consecutive-failure count. Failure time is independent of earlier
success time. An invalid or future failure time makes coverage insufficient without erasing the reported counter. The
no-recorded-success rule warns only when configuration and backup status are reported, the failure count is reported zero,
and the success timestamp is omitted. This describes the observed record; it does not prove that a backup never succeeded.
Malformed or future success times are insufficient, not missing success. Missing status or counters cannot produce this
finding. Backup precedence remains failures, no recorded success, then overdue completion, including when the caller
excludes a higher-priority rule. The adapter does not infer an omitted failure count as zero.

The upgrade rule uses the rolling-upgrade failure object's reported reason. It warns about the recorded failure without
copying the reason or claiming an active failure. Missing or malformed reasons are insufficient. Invalid/future error times
make coverage insufficient while retaining the recorded failure. Absent/null upgrade or failure objects produce no report.
Blue/green phases and job-failure counters remain observations; neither implies a rolling-upgrade failure.

The restore-test rule keeps the active reservation, last result, and `Passed` condition separate. A current `Passed=False`
condition with a reported reason other than `Failed` produces an attention finding before interpreting active or last state.
The condition needs its own current generation acknowledgement; resource acknowledgement does not substitute for it.
An active reservation suppresses the earlier failed result, preserving console precedence. It does not verify the child
request or its progress. Without an active reservation, a reported last `Failed` result produces a recorded-result warning
limited to that snapshot and disposable target. Missing or invalid active state cannot establish that the last result is
applicable. Missing, stale, or invalid condition evidence and missing/invalid/future result times retain insufficient
coverage while preserving independently supported result findings. A missing/invalid outcome does not become `Failed`.
Absent/null restore-test status produces no report; no finding establishes recovery readiness.

StatefulSet readiness requires a confirmed cluster UID edge, current positive generation acknowledgement, known replica
counts, and a nonterminating object. A shortfall warns under the StatefulSet identity. The cluster readiness rule retains
its separate severity policy. A complete supported scope with no confirmed StatefulSet warns under the cluster identity
when current voter intent is positive. Partial reads, unresolved candidates, stale evidence, or missing intent cannot
establish that absence.

Pod findings require a confirmed controller chain through a captured StatefulSet. `Failed` phase produces an error;
current `Ready=False` produces a warning when phase is not failed. A missing/unknown or noncurrent Ready condition makes
coverage insufficient, including when a failed-phase finding remains independently supported. Ready uses the Pod's own
metadata generation and condition acknowledgement. Kubernetes declares condition acknowledgement optional; omission
therefore leaves current Pod readiness unconfirmed. Malformed/future transition time makes coverage insufficient without
erasing a generation-confirmed status. Waiting-container fields add references without copying reasons or messages;
container state alone does not add a new rule. Unknown/malformed phase retains uncertainty alongside independent readiness.

Pod population counts nonterminating confirmed Pods by StatefulSet UID. A shortfall warns under that StatefulSet with
`pod-population`. It requires both complete scopes, resolved associations, current StatefulSet intent, fresh observations,
and verified target stability. Independently missing Pod readiness does not erase a confirmed population shortfall.
Deleting workloads are skipped. Invalid/future deletion times retain insufficient coverage and cannot establish population.

Population/absence coverage accepts an unfiltered namespace LIST or the existing CLI selector
`openbao.org/cluster=<selected-name>`. Findings describe that declared scope, not all cluster resources. Other selectors
retain independent positive observations but make coverage insufficient and suppress population/absence findings. No rule
adds reads, broadens selectors, or changes consumer permissions.

Snapshot-restore findings require a confirmed target/execution UID or request-bound restore-origin annotation before any
hold comparison. Names, creation times, operation IDs, and hold values alone do not confirm ownership. A reported hold
matches a request UID or its recorded operation ID only after UID association. Multiple matching captured requests leave
the hold ambiguous and coverage insufficient; no request is selected by age or list order.

A uniquely matched held request with reported `Unknown`/`Failed` phase, unknown execution stage, or failed cleanup produces
an error. With historical reporting enabled, an unheld recorded concern produces a warning that does not claim active
failure. With it disabled, those historical outcomes produce no finding. Reported `Pending`, `Validating`, or `Running`
phase produces an informational finding. The subject is the request identity; changing severity or policy does not change
its finding key. Display selection remains separate; diagnosis evaluates every confirmed request in the captured scope.

A missing or malformed phase is insufficient and is never inferred from age. An independently unknown held execution can
still support an error. Submission-claim presence, target application, administrator disposition, execution receipts, and
managed restart completion do not supply missing phase or authorize replay. Producer times are checked against that
request's read time, not the earlier cluster read. Future/invalid times or reversed start/completion retain insufficient
coverage while preserving independently reported concerns. Empty findings and `Completed` phase do not qualify end-to-end
recovery or client transactions.

Restore analysis retains the primary LIST outcome. Partial reads can retain independently bound findings; denied reads
cannot resolve a hold. Nonempty restore selectors retain known observations with insufficient coverage. An unmatched or
ambiguous hold remains insufficient. Missing binding capability, stale reads, changed verification, deletion, or replacement
cannot produce current restore findings. Diagnostic text excludes provider messages and raw hold values.

Run `go run ./examples/diagnose` for synthetic evaluation. Fixture and unit coverage do not qualify an operator integration.

## Verification with list-only credentials

`evidence.NewTargetVerification` compares a selected cluster with an already collected named GET or complete, unfiltered
namespace LIST. It performs no I/O. A console can repeat its existing bounded namespace LIST after related reads and
verify every selected target without adding a GET permission. The LIST must retain every candidate, with matching
origin, namespace, group, version, contract, interval, and candidate count. Filtered lists, duplicate identities, and
declarations that omit candidates are rejected. A partial list never verifies a target, even when it includes a match.

A complete LIST confirms unchanged positive generation, changed generation, deletion by name absence, or replacement by
a new UID for the same name. Missing or invalid generations remain insufficient. The verification retains the LIST
scope and collection outcome; it never describes a LIST as a GET. This expands admitted verification evidence in rule
set `7`; older policies are rejected. It does not make the collection atomic or qualify an operator integration.
