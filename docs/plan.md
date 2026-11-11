# Shared client extraction

The extraction ends at milestone 6. Lifecycle features follow separately. The repository is public from its first push;
publication is no longer a later milestone. Initial releases use immutable `v0` tags.

## 1. Define the contract and behavioral baselines

Record the consumer commits in [contract decisions](contract.md). Use the console's broader observations and the CLI's
workload evidence, references, and collection safeguards. Resolve missing values, generations, associations, provider
text, finding identity/severity, time, diagnostic scope, and partial collection before moving their implementations.
Record preserved behavior and intentional corrections separately.

Gate: each difference has a documented decision and an executable expected-result test. Complete through the test-only
reference specifications and archived consumer suites recorded in [behavioral baselines](baselines.md). Production
extraction follows in milestone 3; fixtures do not qualify an operator integration.

## 2. Establish the public repository and development workflow

Use the CLI scaffold and applicable operator engineering and repository controls. Include Apache-2.0 licensing,
attribution, package documentation, examples, pinned tools, checks, dependency notices, and verified GitHub settings.
Require pull requests and qualified checks after the bootstrap. Protect `v*` tags from update and deletion.

Match Go 1.27.4 and use Kubernetes 0.37.2 when Kubernetes dependencies are first required. Do not copy unused application
dependencies. Use a local Go workspace for coordinated development, and keep workspace files untracked.

Gate: local checks and hosted checks pass, configured repository policies are read back, and an external module can
download a published library commit without credentials, with `GOWORK=off`, a fresh module cache, and no replacements.

## 3. Extract projections, collection, and diagnostics

Move small slices after their contract tests pass: cluster identity/conditions/generations/readiness; backup, upgrade,
replication, restore-test and recovery evidence; StatefulSets, Pods, restore correlation, bounded collection; pure rules.
Keep facts separate from interpretation. Include the CLI's status attention logic in the diagnostic inventory.

Implemented: production slices include cluster, operational, and workload evidence; approved JSON projection;
UID associations; timestamps; and collection outcomes. The pinned operator has no public native-recovery or
remote-replication definitions. [Extraction status](extraction.md) records those unsupported capabilities and the implemented
rule inventory. Scoped GET/LIST primitives enforce pagination, byte/candidate/duration
bounds, cancellation, and named UID pinning. Discovery facts and declared related reads now include shared budgets and
final UID/generation verification. Finding identity, coverage, and fifteen supported rules now run through production code
with deterministic replay. Native recovery and remote replication remain reserved. Consumer migration
remains in milestones 4 and 5.

Gate: production tests cover malformed fields, sensitive-data canaries, incomplete reads, pagination/size/duration limits,
replacement, conflicting identities, deterministic ordering, and serialized replay. Local checks and archived consumer
baselines pass. Published-candidate download/build and hosted required checks qualify the final candidate before merge.
Product orchestration remains in consumers; no operator integration is qualified by this gate.

## 4. Migrate the console

Replace duplicated projection and interpretation. Keep enrollment, authorization, transport policy, renewal, scheduling,
history, and persistence in the console. Keep library-to-product mappings focused. Improve provisional JSON or storage
contracts when needed; version changes and document migration or development-reset requirements before applying them.
Preserve useful deployment history and first-seen semantics. Never resolve findings from failed optional reads.

Implemented in console candidate `d079e9f9621ae73fd895ef70f3bfe48cc129ffe0` (PR #24). Local gates pass, including
scoped real operator collection. [Qualification](console-qualification.md) records the installed identities and limits.
The console PR remains open; milestone 4 is not complete. Hosted Dependency Review needs private-repository Code Security,
and the Check workflow has a startup failure. No policy or visibility setting is changed to bypass these failures.
The repository remains private, and PR #24 stays open at the user's request. Resume console qualification and merging
only when the user asks to continue.

Gate: `make check`, generated artifacts, SQLite/PostgreSQL checks, development/production browser workflows, storage
workflows, and an isolated operator collection test execute and pass. Preserve read-only scope and redirect handling.
Document visible changes and regenerate screenshots where needed. A skipped integration check does not pass this gate.

## 5. Migrate the CLI

Replace duplicated projections, associations, attention logic, and diagnostic rules. Keep parsing, kubeconfig loading,
watch orchestration, rendering, thresholds, and process results in CLI wrappers. Preserve the status/doctor permission
boundary, selected API group and UID, bounded timeouts, deletion/replacement behavior, and incomplete-evidence precedence.
Update provisional JSON deliberately instead of retaining duplicate interpretation solely for old shapes.

Implemented in CLI commit `8cb99104e44974fd6bd8311f818cd9d7a1f36bfd`, pinned to published client commit
`74412f30a4c0430092dfd603de15435260e7e86e`. [CLI qualification](cli-qualification.md) records the command boundaries,
intentional corrections, passing local gates, and bounded isolated operator collection. Milestone 5 is complete. GitHub
Actions remains disabled for the private CLI repository; no hosted CLI result is claimed. The operator check covers a
Pending workload and empty restore list, not healthy-cluster diagnosis or nonempty restore correlation. Milestone 4
remains deferred, and milestone 6 has not started.

Gate: command tests verify output, exit codes, actual request scope, timeouts, interruption, replacement, and sensitive-data
exclusion. Run `make check`, `make vulncheck`, dependency-license checks, and distribution verification.

## 6. Pin and qualify one library version

Qualify both consumers against the same candidate commit. Record consumer commits, library commit, operator/CRD/image
identity, rule policy, reference time policy, and executed checks. Tag the exact qualified commit, pin the tag in both
consumers, then repeat independent builds with `GOWORK=off`, fresh module caches, and no replacements.

Gate: both applications pass their required checks against the same immutable tag; dependency notices and distribution
archives are regenerated and verified; the console image builds; shared projections and rules have one implementation;
temporary comparison code is removed. Update [compatibility evidence](compatibility.md) without inflating qualification.

## Subsequent lifecycle features

Implement pause/resume, maintenance, restart, backup, and restore separately in an `operations` package before exposing
them in applications. Use field-scoped changes, UID/resource-version preconditions, bounded waits, interruption/conflict
handling, request correlation, and separate acceptance and observation results. Improve public operator APIs where
identity or correlation is insufficient. Keep pause and maintenance independent; never resume implicitly.

Before console lifecycle features, update its read-only product contract and introduce a separate authorized operations
path with audit records. Observation credentials remain read-only. Add bundle processing with its first implemented
feature and bound untrusted input.
