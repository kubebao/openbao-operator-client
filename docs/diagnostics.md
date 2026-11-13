# Diagnostics

`diagnose.Analyze` evaluates a typed `Capture` with a `Policy` and returns a `Report` of findings and coverage. It reads no
clock, network, environment, credentials, or product state. Identical evidence and policy produce the same report,
whatever the order of subjects, rules, or references. Empty findings do not mean health or complete coverage.

Consumers own severity mapping, rendering, first-seen times, history, and process exit codes.

## Rules

Rule set `9` implements the rules below. `SupportedRules` returns them. Selecting an unknown ID returns `ErrUnknownRule`.
Selecting a reserved ID returns `ErrUnavailableRule`. Changing a rule's semantics or the executable inventory requires a
new rule-set version, and `Analyze` rejects every other version.

| Rule ID | Meaning | Subsubject |
| --- | --- | --- |
| `KBAO-CLUSTER-001` | `Available=False` for the current generation | None |
| `KBAO-CLUSTER-002` | `OpenBaoSealed=True` for the current generation | None |
| `KBAO-CLUSTER-003` | Fewer ready replicas than desired | None |
| `KBAO-WORKLOAD-001` | UID-associated StatefulSet readiness, or no associated StatefulSet | `ready`; `statefulset-population` |
| `KBAO-POD-001` | UID-associated Pod failure, readiness, or missing Pods | `phase`; `ready`; `pod-population` |
| `KBAO-RESTRICTION-001` | Pause, maintenance, restore hold, or break glass | Source field path |
| `KBAO-RESTORE-001` | UID-confirmed snapshot-restore progress or recovery concern | None |
| `KBAO-UPGRADE-001` | Recorded rolling-upgrade failure | None |
| `KBAO-BACKUP-001` | Consecutive backup failures | None |
| `KBAO-BACKUP-002` | Configured backup with no recorded success | None |
| `KBAO-BACKUP-003` | Backup completion age exceeds the policy limit | None |
| `KBAO-RESTORETEST-001` | Failed or blocked restore test | None |
| `KBAO-CONDITION-001` | Other recognized adverse condition | Condition type |
| `KBAO-CONTROLLER-001` | Recorded workload or administrative controller error | `workload`; `adminOps` |
| `KBAO-GENERATION-001` | Stale or inconsistent generation acknowledgement | None |

`KBAO-RECOVERY-001` (native recovery) and `KBAO-REPLICATION-001` (replica history conflict) are reserved IDs. The
supported operator contract has no public API for them, so they cannot produce findings.

## Inputs

A `Capture` holds normalized evidence, not raw resources. Consumers decode JSON and enforce input byte limits. `Analyze`
validates shapes and versions and fails without partial findings or raw values.

Each `ClusterInput` declares:

- The full subject identity and its projection contract.
- The cluster read's `Collection`, and the projected `Observation` when the read produced one. A nil observation keeps
  coverage for a known subject whose read failed or was not requested.
- An optional `Verification` from a later target read.
- Optional `StatefulSets`, `Pods`, and `Restores` resource inputs. Nil means the scope was not requested.

Subject, scope, contract, identity, API version, and verification must agree. A complete read must contain the
observation's read time and a positive candidate count. Duplicate subjects are rejected. Equal UIDs in different origins
are different subjects.

### Related resource inputs

Each `ResourceInput` keeps its projection contract, its LIST `Collection`, and every admitted candidate before UID
filtering, including foreign resources. A complete input has exactly `Collection.Items` observations, and a valid empty
list is a non-nil empty slice. Scope, origin, namespace, resource, version, contract, read times, and counts must agree.
Duplicate UIDs or names are rejected.

Restore reads use the cluster's group, version, and contract. Related reads happen after the cluster read and before the
final verification.

`Analyze` computes associations itself from owner references and reserved annotations, through the `evidence`
implementation. It accepts no caller-supplied association.

## Policy

`DefaultPolicy(reference, contracts)` selects all supported rules and sets these values:

| Field | Default | Constraint |
| --- | --- | --- |
| `MaxBackupAge` | 24 hours | Positive |
| `StartupGrace` | 1 minute | Zero or positive |
| `MaxEvidenceAge` | 5 minutes | Positive |
| `MaxSubjects` | 5,000 | Positive; counts clusters and all workload and restore candidates |
| `ReportHistoricalRestores` | `true` | |

`ApprovedContracts` is the caller's allowlist of projection contracts. Every supplied contract must be in it, including
the contract of an empty or failed read. `RestoreUIDBindingContracts` is a subset of `ApprovedContracts`. Include a
contract only when `adapter.Describe` declares `CapabilityRestoreUIDBinding` for it. Rules and contracts must be unique.
A reference time before collection completion is invalid. The policy is part of the report.

## Coverage

`Analyze` returns one coverage entry per cluster and rule. Each entry records the rule, subject, state, reasons, and the
primary collection outcome.

| State | Meaning |
| --- | --- |
| `excluded` | The policy did not select the rule |
| `not_requested` | The rule needs a scope that was not read |
| `not_applicable` | The evidence shows the rule does not apply, such as no backup configuration |
| `insufficient` | The evidence cannot support a complete evaluation |
| `evaluated` | The rule was fully evaluated; it can have no findings |

`Reasons` lists every insufficiency cause, sorted and deduplicated. `Reason` keeps the primary cause. Reasons include
`scope_incomplete`, `missing_observation`, `stale_evidence`, `missing_field`, `generation_unconfirmed`, `inconsistent_time`,
`target_unverified`, `association_unconfirmed`, and `scope_restricted`.

Findings can coexist with insufficient coverage. For example, a confirmed failed Pod still produces a finding when another
candidate is unconfirmed. Only a complete, valid, empty LIST confirms absence. `ResolveCoverage` derives a coverage state
from selection, requested scope, applicability, and collection validity, for consumers that run their own checks.

### Evidence that cannot support current findings

- Observations read outside the current collection interval or older than `MaxEvidenceAge`.
- A subject whose read reports `deleted` or `replaced`.
- Related-resource findings without `verified` target verification that matches the original reference and generation.

Fresh observations admitted before a later read failure can still support findings, with insufficient coverage. A
consumer can keep earlier findings as last known without treating them as newly confirmed.

## Findings

A finding key is the JSON string array `(rule, origin, group, kind, namespace, name, UID, subsubject)`. Severity, text,
timestamps, policy, and resource versions do not change identity. Findings sort by severity descending, then by key.
References sort by identity, field, API version, resource version, and read time. `CanonicalFindings` copies references
and rejects duplicate keys.

Severities are `info`, `warning`, `error`, and `critical`. Summaries are library-authored text. Provider messages, reasons,
hold values, and instructions never appear in findings.

## Rule behavior

### Cluster conditions

- **Availability** (`KBAO-CLUSTER-001`) needs current resource acknowledgement and current acknowledgement of the
  `Available` condition itself. `Available=False` is a warning. An unconfirmed generation or `Unknown` status is
  insufficient.
- **Sealing** (`KBAO-CLUSTER-002`) needs a current `OpenBaoSealed` condition and a known pause state.
  `OpenBaoSealed=True` is critical. Pause suppresses the finding.
- **Recognized conditions** (`KBAO-CONDITION-001`) cover a fixed list of conditions and polarities. `Degraded=True` and
  `TLSReady=False` are examples of adverse states, which are warnings. Each condition needs its own current
  acknowledgement. Unknown names are ignored. `Available` and `OpenBaoSealed` use only their own rules.
- **Generation** (`KBAO-GENERATION-001`) warns on stale or inconsistent acknowledgement, with separate text. A missing or
  zero acknowledgement is insufficient, not a finding.

A malformed transition time, or one after the read time, makes coverage insufficient but keeps a confirmed finding. A
missing transition time does not block evaluation.

### Readiness

**Replica readiness** (`KBAO-CLUSTER-003`) needs current resource acknowledgement, known desired and ready counts, known
pause state, and a creation time at or before the read. These suppress the finding: pause, being within `StartupGrace`
of creation, zero desired replicas, or enough ready replicas. An age equal to the grace period is outside it. Zero ready
replicas is critical; any other shortfall is a warning.

### Restrictions and controller errors

- **Restrictions** (`KBAO-RESTRICTION-001`) report pause, maintenance, and a reported restore hold as `info`, and active
  break glass as `warning`. Each uses its source field as the subsubject. An unknown or invalid flag makes coverage
  insufficient and keeps the other restrictions. Absent or null objects produce no report. A hold proves no restore
  ownership, and maintenance propagation to workloads is not verified.
- **Controller errors** (`KBAO-CONTROLLER-001`) warn on a reported reason in `status.workload.lastError` or
  `status.adminOps.lastError`. The finding describes a recorded error, not an active failure. A missing or malformed
  reason is insufficient. An invalid or future error time makes coverage insufficient and keeps the finding.

### Backups

All backup rules need reported backup configuration, reported backup status, and a reported failure count. Absent
configuration is `not_applicable`. Missing or invalid values are insufficient; a missing failure count is never zero.

A positive failure count takes precedence. It makes the other two rules `evaluated` with reason
`higher_priority_backup_signal` and no finding, even when the policy excludes `KBAO-BACKUP-001`.

- **Failures** (`KBAO-BACKUP-001`) warn on a positive consecutive-failure count.
- **No recorded success** (`KBAO-BACKUP-002`) warns when the failure count is zero and no success time is recorded. It
  describes the observed record; it does not prove that no backup ever succeeded.
- **Age** (`KBAO-BACKUP-003`) warns when the time since the last success exceeds `MaxBackupAge`. An age equal to the limit
  is within it. Without a recorded success time, this rule is insufficient. Completion age is not a measured recovery point objective.

A malformed success or failure time, or one after the read time, is insufficient.

### Upgrades and restore tests

- **Upgrade failure** (`KBAO-UPGRADE-001`) warns when `status.upgrade.failure` has a reported reason. It does not claim an
  active failure. Blue/green phases and job-failure counts are observations only.
- **Restore test** (`KBAO-RESTORETEST-001`) evaluates in this order:
  1. A current `Passed=False` condition with a reason other than `Failed` is a warning. The condition needs its own
     current acknowledgement.
  2. An active reservation suppresses an earlier failed result.
  3. Otherwise a last result of `Failed` is a warning that covers only that snapshot and disposable target.

  Missing or invalid active state, outcome, or times are insufficient. A missing outcome never becomes `Failed`. No
  finding establishes recovery readiness.

### Workloads and Pods

Workload and Pod rules need a confirmed association chain and `verified` target verification. Each rule has one coverage
entry under the cluster, with its primary LIST outcome. Pod coverage also depends on the StatefulSet read.

- **StatefulSet readiness** (`KBAO-WORKLOAD-001`) warns under the StatefulSet when ready replicas are below desired. It
  needs current acknowledgement, known counts, and no deletion timestamp. When a complete scope has no confirmed
  StatefulSet and the cluster's desired replicas are positive, it warns under the cluster with `statefulset-population`.
- **Pod phase** (`KBAO-POD-001`, `phase`) is an error for a `Failed` Pod.
- **Pod readiness** (`ready`) is a warning for current `Ready=False` when the phase is not `Failed`. It uses the Pod's
  generation and the condition's own acknowledgement. A missing or `Unknown` `Ready` condition is insufficient with
  `missing_field`, and an unacknowledged one with `generation_unconfirmed`. Waiting containers add references without copying reasons.
- **Pod population** (`pod-population`) warns under a StatefulSet when it has fewer non-terminating confirmed Pods than
  desired replicas. It needs both scopes complete, all associations resolved, fresh observations, and a verified target.

Pods excluded from StatefulSet membership, such as backup Job Pods, and foreign Pods do not affect counts or coverage.
Deleting workloads are skipped. An invalid or future deletion time is insufficient.

Population and absence findings need an unfiltered namespace LIST or the selector `openbao.org/cluster=<cluster-name>`.
Any other selector keeps individual findings but makes coverage insufficient with `scope_restricted`. No rule adds reads
or broadens a selector.

### Snapshot restores

`KBAO-RESTORE-001` needs the cluster's contract in `RestoreUIDBindingContracts`, verified target verification, and a
UID-confirmed request. It evaluates every confirmed request; display selection is separate.

A uniquely held request with phase and execution stage `Unknown` is informational during confirmed disposable retention. This requires complete,
unfiltered reads, a UID-confirmed target, reported lifecycle `Disposable`, a positive retention period, terminal result
`Succeeded`, and a valid reported application time. The analysis reference time must precede the application time plus
retention. Cleanup must be absent or `Pending`; a managed restart or administrator disposition prevents this exception.
Failed, invalid, missing, conflicting, or expired evidence keeps the conservative recovery finding. This exception
reports the operator's application and retention record; it does not establish end-to-end recovery.

A reported `openbao.org/restore-hold` matches a request UID, or the request's recorded operation ID, only after UID
confirmation. More than one matching request makes the hold ambiguous and coverage insufficient. No request is chosen by
age or order.

| Condition | Severity |
| --- | --- |
| The held request reports confirmed disposable retention within its configured period | `info` |
| Another held request reports phase `Unknown` or `Failed`, stage `Unknown`, failed execution receipt, or cleanup `Failed` | `error` |
| An unheld request reports the same, and `ReportHistoricalRestores` is `true` | `warning` |
| The request reports phase `Pending`, `Validating`, or `Running` | `info` |

The subject is the request. A missing or malformed phase is insufficient and is never inferred from age. Submission
claims, applied targets, dispositions, and restart completion do not stand in for a phase. Producer times are checked
against the request's own read time. A restore selector, an unmatched hold, or a denied read leaves coverage
insufficient. A reported failed execution receipt remains adverse even when the phase says `Completed`. `Completed` and empty findings do not prove end-to-end recovery.

## Example

`go run ./examples/diagnose` projects a synthetic cluster and prints `2 findings, 15 coverage entries`.
