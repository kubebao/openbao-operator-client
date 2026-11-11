# Common candidate qualification

This record covers local validation on 11 November 2026 with Go 1.27.4. The common client candidate is
`0e0f536c49b4a94c89411b00693aa27672f624b6`, published as `v0.0.0-20261111134438-0e0f536c49b4`.
It includes the pre-release contract corrections in rule set `8`. No client tag exists. Tagging remains on hold.

| Consumer | Exact source commit | State |
| --- | --- | --- |
| CLI | `2cf9ce231081706141488b580d80301681fab4bf` | Pushed to private repository main; GitHub Actions remains disabled |
| Console | `678e4b797ca94cbf8db7924b3bae42496f0ecc49` | Private repository PR #24 remains open |

Both consumers pin this client pseudo-version without replacement directives. Their independent builds use
`GOWORK=off`, separate fresh module caches, the public Go proxy and checksum database, and no GitHub or private-module
credentials. Module download, verification, selected-version checks, and command builds pass.

## Current local checks

| Check | Result |
| --- | --- |
| Client `make check` | Pinned lint, race tests, contract fixtures, and architecture checks pass |
| CLI `make check` | Lint, race tests, and command tests pass, including backup Job Pod exclusion, incomplete-association exit results, validation paths, and sensitive-data exclusion |
| Console `make check` | Go lint/race tests, TypeScript, web lint/format, web unit tests, and asset build pass |
| Console `make check-generated` | Generated outputs match their sources |
| Console browser workflows | `make e2e`: 82 pass, two production-only checks skipped; `make e2e-production`: all 84 pass; `make e2e-storage`: SQLite restart, backup restore, and recovery-code replay pass |
| Console `make helm-check` | All chart variants render and lint; invalid values are rejected |
| Console SQLite/PostgreSQL integration | Native PostgreSQL 18.6 and SQLite persistence, collection, HTTP, and renewal tests pass; optional external integrations remain unqualified |
| Consumer `make licenses`, `make check-licenses` | Generated dependency licenses include the pinned client license and attribution; comparisons pass |
| Consumer `make vulncheck` | CLI reports no vulnerabilities; console reports no reachable or imported vulnerabilities and one vulnerability in an unused required module |
| Consumer distribution archives | Darwin/Linux, amd64/arm64 archives build; file sets, executable permissions, client pins, licenses, attribution, and native version commands pass verification; CLI checksums match |
| Console OCI image | Linux amd64/arm64 image builds; each platform includes the pinned client, notices, source revision label, and non-root user `65532:65532` |

The local OCI image index is
`sha256:98ca85fa3c6f25b9152099491beba806fdb56d1d4ae3e81d17c0b3caae18266a`.
The image and archives are local qualification artifacts, not published releases.

[Kubernetes field qualification](kubernetes-qualification.md) uses a real default Kubernetes 1.34.11 API server.
It confirms current Pod Ready-condition acknowledgement for True and False and completed Job Pod exclusion from
StatefulSet membership. Kubernetes 1.33 is outside the qualification target. Missing acknowledgement remains
`generation_unconfirmed` on any version.

## Outstanding gates

Console CodeQL query execution reaches upload, but GitHub rejects code-scanning results because the private repository
lacks the required security feature. Dependency Review also reports that the repository does not support it.
The repository remains private and its security checks remain enabled. These failures prevent a qualified console merge.

A fresh console operator-integration attempt against the preceding common candidate `1edeecf` collects evidence, but
fails its healthy-state assertion while the disposable OpenBao sample remains uninitialized. Native database integration
passes separately. This attempt does not qualify a healthy operator for the corrected candidate.

The earlier [console](console-qualification.md) and [CLI](cli-qualification.md) operator checks retain their original
commits, environments, and bounded results. They do not qualify the new candidate automatically. Milestones 4 and 6
remain incomplete. The exact tagged version must still pass independent consumer qualification after tagging is authorized.
