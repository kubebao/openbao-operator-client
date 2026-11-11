# Diagnostic evaluation

`diagnose.Analyze` receives a typed `Capture` and `Policy`. It reads no clock, network, environment, credentials, or product
state. Identical evidence and policy produce the same report when subject, selected-rule, or reference order changes.
Empty findings do not establish health or complete coverage.

Rule set `2` implements eight rules: availability, sealing, replica readiness, recognized adverse conditions, generation
acknowledgement, controller errors, restrictions, and backup completion age. `SupportedRules` returns the executable
inventory. Rule set `1` is rejected rather than silently extended. Other canonical IDs remain in the [catalog](rules.md),
but selecting one returns `ErrUnavailableRule`. Unknown IDs return `ErrUnknownRule`. Reserved native-recovery and
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
age limit, 1-minute startup grace, 5-minute evidence age limit, and 5,000-subject limit. Durations and subject limits must
be positive, except startup grace can be zero. Rules and contracts must be unique. A reference time before collection
completion is invalid. These are evaluation parameters, not process exit thresholds.

`Capture` contains normalized value types, not raw Kubernetes resources. `Analyze` validates typed inputs and versions;
consumers own JSON decoding and byte limits. Serialized replay tests use trusted fixtures. They do not implement or qualify
an untrusted bundle parser. Bundle handling remains a separate future package.

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
The backup-failure rule remains pending; selecting only backup age does not evaluate failures. Completion age must exceed
the limit to warn; equality is within the limit. Completion age is not measured recovery point objective (RPO).

Run `go run ./examples/diagnose` for synthetic evaluation. Fixture and unit coverage do not qualify an operator integration.
