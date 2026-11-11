# Behavioral baselines

Baseline tests execute real source from the immutable commits in [the contract](contract.md). They preserve historical
behavior, including behavior the shared contract corrects. Self-contained reference tests execute the intended behavior.
This separation prevents an intentional correction from being described as an unchanged migration.

## Run the archived suites

With the console and CLI Git repositories available locally, run:

```sh
python3 scripts/check-baselines.py \
  --console ../openbao-operator-console \
  --cli ../openbao-operator-cli
```

The script verifies the full commit IDs, archives each commit into a temporary directory, adds the committed baseline
tests, and runs these packages with the race detector, `GOWORK=off`, and read-only module manifests:

- Console: `internal/collector`, `internal/evidence`.
- CLI: `internal/collect`, `internal/doctor`, `internal/evidence`.

It does not switch branches or change consumer worktrees. It uses the normal Go dependency cache. Go 1.27.3 and Python
3.12 or newer are required. Consumer source is not fetched or executed by public library CI. CI runs all self-contained
contract specifications through `make check` without consumer repositories or Kubernetes dependencies.

Both archived suites passed locally on 10 November 2026. This includes all existing tests in those packages and the
additional tests under `internal/contracttest/baseline`. It does not include complete application checks, databases,
browsers, a real Kubernetes API server, or an operator integration environment.

## Preserved behavior

| Behavior | Executable source baseline |
| --- | --- |
| Unknown readiness stays unknown without the documented zero preconditions | Console `TestMissingReadinessRemainsUnknown`, `TestOmittedReadyReplicasCountAsZeroOnceObserved`; CLI `TestContractBaselineProjection` |
| Provider messages are excluded from console evidence; exact Raft indexes survive | Console `TestProjectionPreservesExactIndexesAndDropsProviderMessages`, `TestBackupProjectionKeepsOnlyValidatedFields` |
| Resource and individual condition generations are independent | CLI `TestContractBaselineProjection`; client `TestGenerationContract` |
| All nine console workload/operation rules and severities are recorded | Added console `TestContractBaselineRuleInventory`; existing backup precedence/startup-grace tests |
| Failed full collection keeps unresolved evidence | Console `TestCollectionFailurePreservesCriticalEvidence`, `TestIncompleteCollectionIsNeverHealthy` |
| Labels/names do not establish workload ownership; conflicting restore UIDs are rejected | CLI `TestLabelsAndNamesDoNotProveAssociation`, `TestConflictingRestoreIdentitiesAreNotAssociated`, added `TestContractBaselineAssociation` |
| Partial related reads preserve independent evidence | CLI `TestPartialCollectionPreservesIndependentEvidence` |
| Pagination, size limits, timeout, group/UID pinning, deletion, and replacement stay bounded | CLI `TestDiagnosticPaginationAndLimits`, `TestCollectionTimeoutRemainsIncomplete`, `TestRefreshPinsUIDAndAvoidsRediscovery`, `TestRefreshReportsDeletion`, `TestDiagnosticTargetChangesRemainIncomplete` |
| All five doctor rule groups, incomplete-evidence precedence, thresholds, and stored replay remain recorded | CLI `internal/doctor` suite |
| Status attention is included in the extraction inventory | Added CLI `TestContractBaselineAttentionInventory` covers all 12 attention codes |

## Intentional corrections

| Baseline difference | Extraction decision and expected-result fixture |
| --- | --- |
| Console accepts future generations for zero-readiness inference and sealed findings | Require positive equality. Added baseline tests retain the old result; `generations.json` and `values.json` require unknown/inconsistent applicability. |
| CLI preserves omitted ready counts and pause as unknown even for the inspected schema | Apply only the documented omission policy and retain its provenance: `values.json`. |
| Console coerces some missing/invalid counters and booleans to zero/false | Preserve unknown/invalid values; `values.json`, `conditions.json`. No generic omission default is allowed. |
| Console associates unbound recovery by name/time and can ignore conflicting restore target UID | Retain candidates without ownership; reject conflicting bindings: `associations.json`. Added console `TestContractBaselineRecoveryAssociation` records both old cases. |
| Equal-time console requests select the last input item | Use confirmed requests and a UID tie-breaker for display: `selection.json`. This is not operation correlation. |
| Console optional denied reads become empty requests before reevaluation and can clear recovery findings | Preserve per-scope outcomes and last-known unresolved findings: `coverage.json`, `history.json`. Existing collector tests verify actual denial classification; added recovery tests record reevaluation behavior. |
| CLI redaction still retains arbitrary provider-message canaries | Default projection excludes them: `projection.json`. Optional local text uses a bounded sidecar: `text.json`. |
| Console accepts duplicate/invalid conditions; CLI rejects the whole projection | Invalidate the condition set and retain independent valid fields: `conditions.json`. |
| CLI related-resource projection reads the wall clock internally | Supply resource read time; do not substitute transition or analysis time: `time.json`, `replay.json`. |
| CLI workload ownership compares the owner's complete API version | Compare group, kind, name, and UID across served versions within the same group: `TestControllerReferenceVersionDoesNotChangeIdentity`. Controller and annotation requirements remain. |
| CLI Pod ownership ignores conflicting cluster-owner annotations | Check the annotation against the cluster root in addition to the StatefulSet controller reference: `TestControllerChainChecksBothUIDLevels`. |
| CLI permits a matching `restore-hold` value as ownership proof | Require target/execution UID or request-bound `restore-origin`; a hold operation ID alone remains a candidate: `associations.json`. |
| CLI list handling fills empty item API version/kind from the response scope | Inherit only omitted type metadata from a validated typed list. Reject present null, empty, or conflicting declarations: `TestMalformedListEnvelopes`. |
| CLI discovery accepts a matching resource without checking its group/version or advertised verbs | Validate the discovery envelope and keep reported read verbs separate from authorization and integration qualification: `TestDiscoveryRetainsFactsWithoutQualifyingCapabilities`. |
| The milestone 1 collection model returns source order on a pagination loop | Sort partial and complete observations by identity. The `repeated-token` fixture now expects `a`, then `b`. |
| The milestone 1 collection model calls unclassified provider failures invalid input | Classify unknown transport errors as `failed` without raw text. The `unclassified-provider-error` fixture changes only its outcome. |
| Console issue IDs are deployment/rule pairs; CLI findings have no subject identity | Adopt subject/subsubject keys and deliberate console mappings: `findings.json`, `history.json`. Preserve first-seen for one-to-one mappings only. |
| CLI snapshots and reports include command exit codes and thresholds | Keep those fields in wrappers: `projection.json` and replay boundary tests. Container termination codes remain evidence. |
| CLI status attention and doctor can interpret the same fields separately | Use the combined [rule catalog](rules.md) and map missing evidence to coverage. |
| Console assumes native recovery, remote replication, and `RestoreSubmitted` fields absent from the pinned API | Keep those paths unsupported. Native association fixtures require unconfirmed results. Production tests reject the kind, omit remote fields, and mark the stage invalid. See [source corrections](extraction.md#source-corrections). |

Fixture filenames in the corrections table are under `internal/contracttest/testdata`, except generation fixtures under
`evidence/testdata`. Reference projection uses a small normalized field subset; it is not an implementation of the full
operator schema. Existing consumer tests retain the broader backup, upgrade, replication, recovery, and workload scenarios.

## Required sequences

`history.json` covers recovery failure followed by denial, partial reads with independently supported findings, complete
empty results, repeated multi-subject findings, replacement, identity mapping, and a new episode after resolution.
`ownership.json` covers cluster and StatefulSet replacement. `selection.json` and `replay.json` reverse resource/request
order and rule selection. `conditions.json` normalizes condition order. Fixed reference time and policy make serialized
analysis byte-for-byte repeatable. Coverage retains full resource identity, including when two Kubernetes origins report
the same UID. Unknown versions and duplicate resource identities are rejected.

## Extraction handoff

Milestone 1's contract and fixture gate is complete. Repository foundation was implemented ahead of this contract work.
No consumer migration, public operator API change, library tag, or operator qualification is part of this milestone.
Milestone 3 implements the recorded decisions and expands production tests to full schemas and actual read primitives.
Milestones 4 and 5 repeat consumer checks against library calls, including permission boundaries and product history.
