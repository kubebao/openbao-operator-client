# Diagnostic rule catalog

`KBAO-RECOVERY-001` and `KBAO-REPLICATION-001` are reserved inventory. The pinned operator defines neither native recovery
nor remote-replication observations. These IDs must not produce findings under this contract; see
[source corrections](extraction.md#source-corrections).

These canonical IDs define the extraction target. Rule set `1` implements `KBAO-CLUSTER-003` and `KBAO-BACKUP-003`; the
remaining rules are pending or reserved. See [evaluation behavior](diagnostics.md). The archived consumer tests cover
existing interpretation. `internal/contracttest/testdata/rules.json` records consumer mappings. Replay, identity,
severity, and history fixtures define the shared boundaries.

| Canonical rule ID | Meaning | Existing source |
| --- | --- | --- |
| `KBAO-CLUSTER-001` | Operator-reported availability | CLI doctor cluster rule |
| `KBAO-CLUSTER-002` | Current sealed condition | Console `sealed`; CLI sealed attention |
| `KBAO-CLUSTER-003` | Operator-reported replica readiness | Console `replica-readiness` |
| `KBAO-WORKLOAD-001` | Associated StatefulSet readiness | CLI doctor workload rule |
| `KBAO-POD-001` | Associated Pod readiness, failure, or missing population | CLI doctor Pod rule |
| `KBAO-RESTRICTION-001` | Pause, maintenance, restore hold, or break-glass report | CLI doctor restrictions and status attention |
| `KBAO-RESTORE-001` | Confirmed snapshot-restore progress or recovery concern | CLI doctor restore rule; console snapshot `recovery-investigate` |
| `KBAO-RECOVERY-001` | Confirmed native recovery concern | Console native `recovery-investigate` |
| `KBAO-UPGRADE-001` | Recorded upgrade failure | Console `upgrade-failed`; CLI upgrade attention |
| `KBAO-BACKUP-001` | Reported consecutive backup failures | Console `backup-failed`; CLI backup attention |
| `KBAO-BACKUP-002` | Configured backup with no recorded success | Console `backup-never-succeeded` |
| `KBAO-BACKUP-003` | Backup completion age exceeds policy | Console `backup-overdue` |
| `KBAO-RESTORETEST-001` | Failed or blocked restore test | Console `restore-test` |
| `KBAO-REPLICATION-001` | Reported replica history conflict | Console `replica-history-conflict` |
| `KBAO-CONDITION-001` | Other recognized adverse conditions | CLI `reported_condition` attention |
| `KBAO-CONTROLLER-001` | Reported workload or administrative controller error | CLI `controller_error` attention |
| `KBAO-GENERATION-001` | Stale or inconsistent generation acknowledgement | CLI generation attention |

Preserve existing CLI doctor IDs. Preserve the console's first-applicable backup precedence: failures, no recorded
success, then overdue completion. Unsupported, missing, and invalid evidence do not become a successful backup result.

Use the [identity and severity contract](contract.md#findings-and-severity). A restriction uses its field as a subsubject.
Controller errors use `workload` or `adminOps`. Generic conditions use their type. Container concerns use container kind
and name. Pod population findings use the StatefulSet subject and a `pod-population` subsubject.

CLI status attention does not become a second diagnostic engine:

- Map `paused`, `maintenance`, `restore_hold`, and `break_glass` to restriction findings.
- Map `backup_failures`, `upgrade_failure`, and `controller_error` to their matching rules.
- Map `generation_unknown`, `availability_unknown`, and `condition_generation_unconfirmed` to insufficient coverage.
  Generation mismatch retains stale versus inconsistent distinctions under `KBAO-GENERATION-001`.
- Map recognized adverse conditions to `KBAO-CONDITION-001`, except availability and sealing, which use their specialized
  rules. Do not duplicate the same condition in generic and specialized rules.

The console's `collection` issue maps from coverage in its wrapper. Library analysis does not contain console connection
health, first-seen timestamps, CLI thresholds, or process exit codes. Historical failures remain identifiable observations;
they do not prove a currently active failure or authorize replay of an operation.
