# Cluster controls

The `operations` package prepares and submits pause/resume, maintenance, and rolling-restart requests. It uses a separate write client;
`collect.Client` remains read-only. The operator owns reconciliation and lifecycle safety.

## Prepare and review a request

1. Select `adapter.OpenBaoV1Alpha1` for `openbao.org` or `adapter.KubeBaoV1Alpha1` for `kubebao.org`.
2. Collect the target cluster under the selected contract.
3. Call `PreparePause(cluster, contract, paused)` or `PrepareMaintenance(cluster, contract, enabled)`.
4. Call `Plan.Patch()` to review the exact JSON Patch.
5. Configure `operations.Client` with the intended API endpoint and an authorized transport.
6. Submit the plan with bounded `SubmitOptions`.
7. Collect new observations and assess the requested state.

A plan binds the adapter selection, origin, API group, namespace, name, UID, resource version, and positive generation.
Preparation rejects reported or invalid deletion timestamps. Only the two inspected source contracts are accepted, with the matching API
group and version. Unqualified contracts cannot prepare controls. Preparation does not check
RBAC, admission, or operational eligibility. The selected adapter remains in the plan for response projection; it is not
evidence provenance or permission to submit a request.

The patch tests UID, resource version, and generation before changing the selected field. Pause/resume changes only
`spec.paused`. Maintenance changes `spec.maintenance.enabled`; when its parent is absent or null, the patch creates the
maintenance object. A reported parent is preserved. The plan never resumes reconciliation as a side effect.

Serialized plans require a non-null `enabled` value and canonical field names. Decoding rejects unknown or duplicate
keys, invalid UTF-8, and plan values over 16 KiB. Callers must also bound the complete input document before decoding.
Validate plans before displaying or submitting them. Plan validation grants no authority; consumers own approval and
the correspondence between the reviewed plan and the submitted plan.

Run a preview without network access:

```sh
go run ./examples/controls
```

## Rolling restart

`PrepareRestart(cluster, contract, marker)` returns a separate `RestartPlan`. Supply a new RFC 3339 timestamp, generated
once by the caller. Preparation rejects an unchanged marker, invalid identity or parent shape, and a deleting target.
It requires current generation acknowledgement, reported requested and accepted `RollingUpdate` strategies, and no
Blue/Green status. The conservative client policy also refuses paused reconciliation, a restore hold, an operation
lock, or active or unknown break-glass state. Maintenance alone does not block restart.

`RestartPlan.Patch` tests UID, resource version, and generation, then changes only `spec.runtime.restartAt`. When
`spec.runtime` is absent or null, it creates that object with the requested marker. A reported runtime object keeps its
other fields. `Client.SubmitRestart` sends at most one PATCH and supports the same server dry-run, bounds, and outcome
classifications as `Submit`. An accepted response must contain the marker on the selected target with a later generation.

After an unknown submission outcome, retain the same plan and marker. Collect new evidence to assess the requested
state. The client never retries the mutation or changes pause, maintenance, restore holds, or other restrictions.

`AssessRestart(plan, cluster, related, policy)` evaluates supplied evidence without I/O. It needs complete, fresh
StatefulSet and Pod scopes and final target verification. The result separates configured marker state from observed
rollout convergence. Convergence requires all of these facts:

- The selected cluster retains its identity, acknowledges its generation, and reports the requested marker.
- The stable voter pool and every configured steady read-replica pool have confirmed UID associations and exact desired
  Pod populations. A zero-replica read pool retains its StatefulSet and has no live Pods.
- Every pool uses `RollingUpdate` without a partition that excludes ordinals, acknowledges its generation, and reports
  equal, nonempty current and update revisions.
- Each StatefulSet template and every live owned Pod report the requested restart marker. Each Pod's
  `controller-revision-hash` matches its StatefulSet update revision.
- Replica counts match the desired population. Every live owned Pod is running, nonterminating, and has a current
  `Ready=True` condition. A positive `minReadySeconds` also needs matching available-replica evidence.

Missing, malformed, stale, conflicting, or incomplete evidence cannot establish convergence. Deleting workloads remain
pending. A changed target, interrupted reads, or an unsupported topology prevents an observed result.

Blue/Green voter StatefulSets use `OnDelete`; this API does not support their restart workflow. The client does not delete
Pods. The operator and StatefulSet controller perform supported rollouts. With steady read replicas, the operator
converges the read pool before propagating the marker to voters.

An observed result describes current rollout convergence. It does not establish that this request caused historical Pod
replacement, prove serving health or Raft quorum, or reserve a lifecycle operation. Runtime qualification compares Pod
UIDs before and after the request separately. The client operation-lock guard is a conservative precondition, not an
operator guarantee that every concurrent runtime change is serialized.

## Submit once

`Client.Submit` sends at most one PATCH. It does not discover another API group, reload credentials, refresh a stale
plan, follow redirects, retry, or wait for reconciliation. Its caller-configured transport must honor cancellation and
must not retry or follow redirects. Limits bound request duration and response bytes. Responses are projected before
their approved identity and generation enter a receipt; raw responses and provider errors are excluded.

Set `SubmitOptions.DryRun` to request Kubernetes server dry-run. A dry-run receipt is hypothetical and must not be used
as a persisted observation.

| Outcome | Meaning |
| --- | --- |
| `accepted` | The API response contains the requested value on the bound target. Reconciliation is not confirmed. |
| `dry_run_accepted` | The API returned the requested dry-run result. No persistence is established. |
| `denied` | HTTP 401 or 403. |
| `conflict` | HTTP 409 or 412. The client does not refresh or resubmit. |
| `not_found` | HTTP 404. This does not establish deletion of the previously observed UID. |
| `invalid` | HTTP 400, 415, or 422. HTTP 422 can mean validation failure or a failed JSON Patch test. |
| `rate_limited` | HTTP 429. The client does not retry. |
| `not_submitted` | Cancellation prevented the transport call. |
| `unknown` | The response cannot establish whether the patch persisted. This includes ambiguous transport and server failures. |

After `unknown`, read the bound target. Do not infer failure or resubmit automatically. A later observation can show
the requested state, but it cannot establish which actor wrote that state.

## Assess observations

`AssessPause` and `AssessMaintenance` perform no I/O. Supply the plan, a later cluster observation, and an
`AssessmentPolicy` with a reference time, maximum evidence age, and candidate bound. Maintenance also needs the
declared related reads and final target verification from `collect.Related`.

The result separates `Configured` from `OperatorObserved`. These booleans compare supplied evidence with the requested
state. They do not prove authorization or attribute a change to a submission receipt.

| Assessment state | Meaning |
| --- | --- |
| `observed` | The supplied evidence confirms the requested state within the supported assessment scope. |
| `pending` | Known configuration, producer acknowledgement, annotations, or workload population do not yet match. |
| `blocked` | Pause or deletion prevents the assessment from confirming propagation. |
| `insufficient` | Evidence is missing, invalid, stale, incomplete, unverified, or outside the supported topology. |

Pause assessment requires a current resource acknowledgement and a current `Available` condition. For pause, the
condition must be `Unknown` with reason `Paused`. For resume, it must have an evaluated status and a reported reason
other than `Paused`. Resumed reconciliation does not prove cluster health. Pause does not stop Kubernetes controllers,
existing Jobs, or OpenBao processes.

Maintenance assessment verifies annotations on every UID-confirmed StatefulSet and Pod in complete declared reads.
It recomputes associations and requires final target verification with an unchanged cluster resource version. Blue/green
status can change without a generation change; UID and generation alone cannot confirm a stable topology. A resource
version change leaves coverage insufficient, including when an unrelated status update caused it.
Names and labels alone do not establish ownership.
Enabling requires `openbao.org/maintenance=true`; disabling requires annotation removal. Known missing annotations or
Pods are pending. A paused cluster reports a propagation blocker without resuming it.

The supported topology has one voter StatefulSet and one read-replica StatefulSet when `spec.readReplicas` is configured.
A configured zero-replica read pool still requires its StatefulSet and permits no live Pods. Each pool needs its own
reported desired count and exact live Pod population; surplus voters cannot supply a missing read pool.

After UID confirmation, the assessment checks pool and revision agreement between each StatefulSet selector, its Pod
template, and its Pods. Missing or invalid grouping fields, contradictory revisions, and duplicate pool slots leave
coverage insufficient. Confirmed foreign resources and Job Pods are excluded; unresolved associations prevent a
complete assessment. Unexpected retained pools do not establish the desired topology.

An absent blue/green status selects an unrevisioned voter pool. A reported blue/green status must have phase `Idle`,
no Green revision, and a stable Blue revision that agrees with the voter pool. An omitted Blue revision in this valid
Idle object selects the original unrevisioned pool under the pinned operator contract. Current spec counts govern the
settled topology; retained Blue replica counts and operation IDs do not establish it. Active transition phases remain
insufficient. No assessment establishes upgrade safety, serving health, or quorum.

Maintenance mode permits controlled direct resource changes only when the caller also has the required Kubernetes
permissions and the operator's delegated `maintenance` permission. Submitting the flag does not grant those permissions.

See [compatibility](compatibility.md#cluster-controls) for validation limits.
