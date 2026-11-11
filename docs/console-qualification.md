# Console migration candidate qualification

Console candidate `d079e9f9621ae73fd895ef70f3bfe48cc129ffe0` imports client
`7926f09468de82a04e5322b2062e1d579f9fbdf4` through published version
`v0.0.0-20261111084856-7926f09468de`. Local qualification ran on 11 November 2026 with Go 1.27.4.
The console pull request remains open. This record does not mark milestone 4 complete.

## Validation

The console passes `make check`, `make check-generated`, `make integration` with SQLite and PostgreSQL 18,
`make e2e` (82 passed; two production-only tests excluded), `make e2e-production` (84 passed), `make e2e-storage`,
`make screenshots`, `make helm-check`, `make vulncheck`, dependency notice generation/verification, an image build,
and an independent module build with `GOWORK=off` and no replacement directive. The Go vulnerability check finds no
reachable vulnerability; it identifies one vulnerability in an unused required module.

An isolated kind cluster named `console-client-m4` runs Kubernetes 1.36.1 on Linux/arm64. Operator/chart 0.6.1 uses
single-tenant mode for `console-sample`. The reader can LIST clusters and GET the named controller Deployment.
Authorization checks deny cluster GET and Pod LIST. The collector performs no discovery expansion or workload mutations.
The disposable cluster and PostgreSQL service are removed after validation.

| Installed artifact | Identity |
| --- | --- |
| Operator chart | `ghcr.io/kubebao/charts/openbao-operator:0.6.1`, digest `sha256:03bc763ade61590f3616bff2738b379453554084cee12aa61035503c3d9fc338` |
| Running operator image | `ghcr.io/kubebao/openbao-operator@sha256:8a73e5a7c0f13d6786e7ca6d65af20cb36620d4464dc07a6e8210e59d17bcb97` |
| Cluster CRD spec SHA-256 | `7c3659f144c375092d7d1629d65004c15cc30ec41dd81e04a400842ada82ed14` |
| Restore CRD spec SHA-256 | `d3c781479988c032e00e45c6ec4c29f66aba1d11413f65f11f98dd0901b0ca64` |

CRD hashes cover sorted, compact JSON of each installed `spec`. Tests run through a short-lived scoped ServiceAccount
credential, not the administrator kubeconfig:

```sh
CONSOLE_TEST_DATABASE_URL='<isolated console_test DSN>' \
CONSOLE_TEST_KUBECONFIG='<scoped reader kubeconfig>' \
CONSOLE_TEST_CONTEXT=kind-console-client-m4 GOWORK=off make integration
```

`TestKindOperatorProjection` checks real identity, version, replica and condition projections, final LIST verification,
controller assignment, complete selected analysis, and healthy state when the operator reports one ready replica at
generation 1. `TestKindPostgresAndOIDCSession` exercises collection, persistence, authorization, and HTTP output together
with a signed TLS test identity provider.

The empty restore LIST exercises collection only. It does not qualify restore association or execution against a live
request. Backup, upgrade, and restore-test rules retain synthetic coverage in this run. Native recovery, remote
replication, lifecycle operations, external Keycloak, and real credential renewal are not qualified.

## Migration effects

The library owns approved OpenBao projections, bounded resource reads, UID associations, and pure diagnostic rules.
The console keeps enrollment, named controller projection, credential renewal, scheduling, persistence, authorization,
HTTP mapping, and rendering. Its eight selected rules do not require workload reads.

Deployment and singleton finding IDs remain stable. Restore findings use request-specific canonical identities.
Incomplete analysis retains prior unresolved episodes and their last confirmed evaluation time. History and activity
compare finding IDs. New observations use completion time separately from attempt and resource collection time.

The provisional HTTP model accepts unknown backup counters, restore-test outcomes, and recovery cleanup, plus an
optional incomplete-analysis flag. Old persisted JSON remains readable without a database schema migration. New
observations remove name/time-based ownership, inferred pending phases, future generations treated as current, and
unsupported native-recovery/remote-replication fields. Provider messages remain excluded.

## Remaining gate

The private console repository reports Dependency Review unavailable without its security entitlement. Its Check
workflow also has a zero-job startup failure. The inherited organization allowlist omits its pinned
`pnpm/action-setup@ea17c68df8912ef543352723c149a84f56e3d413` and
`helm/chart-testing-action@720270857a28afd41e2426521a7801db09e9b510` Actions. GitHub rejects repository-level changes
because the organization controls this policy. Local validation does not replace hosted results.

The user chose to keep the console private and leave PR #24 open. Console qualification and merging are deferred until
the user asks to continue. No security entitlement, visibility, or Actions policy change is authorized by this decision.
The CLI migration and common immutable `v0` tag remain milestones 5 and 6.
