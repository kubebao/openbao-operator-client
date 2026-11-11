# Cluster controls

The `operations` package prepares and submits pause/resume and maintenance requests. It uses a separate write client;
`collect.Client` remains read-only. The operator owns reconciliation and lifecycle safety.

## Prepare and review a request

1. Collect the target cluster under `adapter.V1Alpha1Snapshot`.
2. Call `PreparePause(cluster, paused)` or `PrepareMaintenance(cluster, enabled)`.
3. Call `Plan.Patch()` to review the exact JSON Patch.
4. Configure `operations.Client` with the intended API endpoint and an authorized transport.
5. Submit the plan with bounded `SubmitOptions`.
6. Collect new observations and assess the requested state.

A plan binds the origin, API group, namespace, name, UID, resource version, and positive generation. Preparation rejects
reported or invalid deletion timestamps. Only the qualified source contract is accepted. Preparation does not check
RBAC, admission, or operational eligibility.

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
