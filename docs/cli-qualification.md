# CLI migration qualification

CLI commit `8cb99104e44974fd6bd8311f818cd9d7a1f36bfd` imports client
`74412f30a4c0430092dfd603de15435260e7e86e` through published version
`v0.0.0-20261111112413-74412f30a4c0`. Local qualification ran on 11 November 2026 with Go 1.27.4.
The signed CLI commit is pushed to its private repository's `main` branch, as its initial-development rules require.
This completes the milestone 5 local gates. It does not complete the deferred console migration or milestone 6.

## Implementation and scope

The CLI uses shared approved-field projections, generation semantics, UID associations, bounded collection, status
attention rules, and doctor rules. Kubeconfig loading, credential plugins, target selection, watch orchestration,
rendering, thresholds, and process exit codes remain in CLI wrappers. Pure snapshots and diagnostic reports contain no
command process result. No duplicate supported projection or diagnostic engine remains in the CLI.

| Command | Collection scope |
| --- | --- |
| `status` | Discovery and one named cluster GET; watch retains the selected group and UID without rediscovery |
| `doctor` | Status, namespace StatefulSet and Pod LISTs using `openbao.org/cluster=<name>`, a namespace restore LIST, and a final cluster GET that verifies UID and generation |

Related reads use pages of 100 and at most 1,000 candidates per kind. Each kind has a 16-request limit, 16 MiB transferred
and retained byte limits, and an 8 MiB response limit. The related sequence shares limits of 64 requests, 3,000 candidates,
and 48 MiB transferred and retained bytes. It reserves the final verification read. The command deadline includes the
initial status read and defaults to 30 seconds. Collection adds no Secret reads, Pod logs or exec, OpenBao requests,
mutations, or broader namespace discovery.

## Contract corrections

- The CLI accepts only the inspected `openbao.org/v1alpha1` contract. It rejects the scaffold's speculative `kubebao.org`
  alias before loading credentials.
- Provider messages are excluded from text and JSON. Legacy message fields remain empty. No optional local text policy
  is implemented.
- Shared rules distinguish resource acknowledgement from condition acknowledgement. Missing, stale, invalid, and
  inconsistent facts limit rule coverage. Unsupported restore reads do not establish inapplicability.
- Restore holds remain restrictions. They cannot establish ownership without the required UID-bound association.
- Client PR #17 corrects proven foreign Pod ancestry and rejects caller assertions that conflict with StatefulSet
  metadata. Consistent foreign chains are excluded from target coverage; conflicting annotations remain incomplete.
- Replacement status is not returned under the selected UID. Watch preserves the original identity and reports the
  replacement as incomplete collection.
- Command JSON retains its `v1` envelope and exit-code fields. It adds stable subject-specific finding IDs, evaluation
  policy, and normalized evidence. Normalized evidence retains all admitted approved candidates, including foreign
  candidates, for counts and deterministic replay. The legacy related view contains confirmed associations and
  metadata-only omissions.

The library evidence schema is `2` and rule-set version is `7`. Status returns zero after a complete read even when the
cluster needs attention. Doctor applies the caller's threshold; incomplete collection or rule coverage takes precedence
over findings. Independent confirmed findings remain available when another read or rule is incomplete.

## Local validation

The following checks pass against the published dependency with `GOWORK=off` and no replacement directives:

```sh
make check
make vulncheck
make licenses
make check-licenses
make dist VERSION=milestone-5
go mod verify
```

Command tests cover text and JSON output, thresholds and exit precedence, request paths and selectors, denied reads,
pagination and candidate limits, generation changes, deletion and UID replacement, watch cancellation, timeouts,
redirects, output failures, and sensitive-data exclusion. Pure tests replay serialized normalized evidence with fixed
policy and analysis time. The vulnerability check finds no vulnerabilities.

Four archives build for Linux and macOS on amd64 and arm64. Verification checks every archive against `checksums.txt`,
checks executable mode and the exact executable/license file set, and confirms the client license and upstream NOTICE
are included. The native macOS/arm64 executable reports CLI commit `8cb99104e44974fd6bd8311f818cd9d7a1f36bfd` and Go 1.27.4.
These are local test archives, not a release.

Dependency notices are regenerated after removing unused Kubernetes dynamic/discovery dependencies. The license
generator now ignores the CLI at a package boundary. Its previous module-name prefix also matched the client module.
An inclusion guard checks that the client license is present, and adjacent upstream NOTICE files enter the notices.

Local logs and archives remain in the ignored CLI paths `.local/milestone-5` and `dist`. GitHub Actions remains disabled
for the private CLI repository. These results do not claim hosted CLI CI. The library candidate's PR #17 passes its four
exact-head required checks; its signed squash merge matches the tested preview tree.

## Bounded operator integration

A disposable kind cluster named `cli-client-m5` runs Kubernetes 1.36.1 on Linux/arm64. Operator/chart 0.6.1 manages a
development cluster named `cli-bao` in namespace `cli-sample`. The initial attempt stopped when OrbStack's Docker socket
disappeared. Testing resumed after Docker returned, without restarting the shared service.

| Installed artifact | Identity |
| --- | --- |
| Operator chart | `ghcr.io/kubebao/charts/openbao-operator:0.6.1`, digest `sha256:03bc763ade61590f3616bff2738b379453554084cee12aa61035503c3d9fc338` |
| Running operator image | `ghcr.io/kubebao/openbao-operator@sha256:8a73e5a7c0f13d6786e7ca6d65af20cb36620d4464dc07a6e8210e59d17bcb97` |
| Cluster CRD spec SHA-256 | `7c3659f144c375092d7d1629d65004c15cc30ec41dd81e04a400842ada82ed14` |
| Restore CRD spec SHA-256 | `d3c781479988c032e00e45c6ec4c29f66aba1d11413f65f11f98dd0901b0ca64` |

CRD hashes cover sorted, compact JSON of each installed `spec`. Tests use ten-minute namespace ServiceAccount tokens.
The status reader can GET the named cluster and cannot LIST Pods. The doctor reader adds namespace StatefulSet, Pod,
and restore LIST permissions. Authorization checks deny Secret GET, cluster PATCH, and cluster LIST.

- Status exits zero after discovery and a named GET. Watch preserves the UID, performs only a GET on its second poll,
  and exits zero after SIGINT.
- Doctor with status-only credentials reports denied related reads and exits three. Its independent cluster warning
  remains available.
- Doctor with full reader permissions collects one StatefulSet, one Pod, and an empty restore list. The final GET verifies
  the selected UID and generation 1. All collection steps complete. The Pending Pod lacks required readiness evidence;
  the Pod rule remains incomplete, and doctor exits three while retaining cluster and workload warnings.

Approved reports and summaries remain under ignored `.local/milestone-5`. The disposable cluster is deleted, scoped
credential files are removed, and existing environments are preserved. This check does not qualify healthy-cluster
diagnosis, nonempty restore correlation, lifecycle operations, backup/upgrade execution, or another operator version.

Console PR #24 stays private, open, and deferred. A common immutable client `v0` tag and independent qualification of both
consumers remain milestone 6.
