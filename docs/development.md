# Development

Use Go 1.27.4 or later, Make, Bash, and Python 3. Go 1.27.4 is the minimum because it fixes the standard-library advisory
`GO-2026-6859`. Go selects the toolchain with `GOTOOLCHAIN=auto`. Repository administration also needs an authenticated
`gh` with administration access.

## Checks

| Command | Checks |
| --- | --- |
| `make check` | Lint, race tests, build of packages and examples, architecture boundaries, tooling tests, and workflow validation |
| `make check-modules` | Module manifests match `go mod tidy` |
| `make vulncheck` | Reachable Go and standard-library vulnerabilities |
| `make licenses` | Regenerates dependency notices for the library, examples, and Go runtime |
| `make check-licenses` | Dependency notices match the generated output |
| `make consumer REVISION=<published-commit>` | Unauthenticated build of the published module from fresh caches |
| `python3 scripts/repository-settings.py --check` | Live GitHub settings match `.github/repository` |

Pinned tools are golangci-lint `v2.14.0`, govulncheck `v1.9.0`, go-licenses `v2.0.1`, and actionlint `v1.7.12`.
Formatting uses gofumpt, goimports, and golines with a 140-character line limit.

## Consumer development

To develop against local console and CLI checkouts, create a workspace outside the three repositories:

```sh
go work init ./openbao-operator-client ./openbao-operator-console ./openbao-operator-cli
```

Workspace builds are for feedback only. Final consumer builds pin a published version with `GOWORK=off` and no
replacements. The module is public and needs no `GOPRIVATE` setting or credentials. Do not match it with a broader
private module prefix.

`make consumer` disables Go authentication, ignores system and global Git configuration, clears GitHub, Git, and SSH
environment variables, and resolves the module through the public proxy without a direct Git fallback. It compiles all
packages at the requested published revision, including feature packages that its example program does not import.

## Kubernetes field check

`internal/integration` checks the Pod fields that the [Kubernetes floor](compatibility.md#kubernetes) depends on:

- The kubelet reports current `observedGeneration` on `Ready=True` and `Ready=False` conditions.
- A completed Job Pod's controller reference excludes it from StatefulSet membership.

The tests skip unless `OPENBAO_CLIENT_TEST_KUBECONFIG` is set. Run them only against a disposable cluster:

```sh
kc="$PWD/.local/kubernetes-1.34/admin-kubeconfig"
mkdir -p .local/kubernetes-1.34
kind create cluster --name client-k134 --kubeconfig "$kc" \
  --image kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d
kubectl --kubeconfig "$kc" create namespace client-k134
kubectl --kubeconfig "$kc" apply -f internal/integration/testdata/workloads.yaml
kubectl --kubeconfig "$kc" -n client-k134 wait --for=condition=Ready pod/ready-true --timeout=60s
kubectl --kubeconfig "$kc" -n client-k134 wait --for=jsonpath='{.status.phase}'=Running pod/ready-false --timeout=60s
kubectl --kubeconfig "$kc" -n client-k134 wait --for=condition=Complete job/backup --timeout=60s
OPENBAO_CLIENT_TEST_KUBECONFIG="$kc" GOWORK=off go test -v -count=1 ./internal/integration
kind delete cluster --name client-k134 --kubeconfig "$kc"
```

The commands pass the disposable kubeconfig explicitly and leave your default kubeconfig unchanged. Both tests must pass.
The fixture uses kind 0.33.0 and digest-pinned images, and it reads no Secrets.

## Operator control qualification

`TestOperatorControls` writes to four caller-provisioned clusters in a disposable Kind environment. It checks real
server dry-run, RBAC denial, JSON Patch preconditions, cancellation, and a discarded response after an accepted PATCH.
It then checks pause/resume acknowledgement and maintenance annotations through the shared collectors and assessments.
An ordinary `make check` skips this test. A skipped test does not qualify controls.

Provision the operator with its admission policies enabled. Use a namespace named `client-controls-<suffix>` and the
matching context `kind-client-controls-<suffix>`. Provide four reconciled, unpaused clusters with maintenance disabled:

1. A voter pool with no configured read pool.
2. A voter pool with a positive read-replica count.
3. A voter pool with a configured zero-replica read pool.
4. A settled blue/green cluster with reported phase `Idle`.

Create a `controls-reader` ServiceAccount in that namespace with cluster GET and LIST permission and no PATCH permission.
Run a loopback `kubectl proxy` with the disposable kubeconfig and context. The proxy must forward the test transport's
`Impersonate-User` header so the API server evaluates the reader's permissions. Verify the proxy's target before running
the test; the test validates the declared scope names but cannot verify which kubeconfig started a proxy.

For example, with the native operator and fixture names `voter`, `read-pool`, `zero-read`, and `blue-green`:

```sh
OPENBAO_CLIENT_CONTROLS_ENDPOINT=http://127.0.0.1:18091 \
OPENBAO_CLIENT_CONTROLS_CONTEXT=kind-client-controls-native \
OPENBAO_CLIENT_CONTROLS_NAMESPACE=client-controls-native \
OPENBAO_CLIENT_CONTROLS_GROUP=kubebao.org \
OPENBAO_CLIENT_CONTROLS_FIXTURES=voter,read-pool,zero-read,blue-green \
OPENBAO_CLIENT_CONTROLS_DENIED_USER=system:serviceaccount:client-controls-native:controls-reader \
GOWORK=off go test -count=1 -timeout=15m -v ./internal/integration -run '^TestOperatorControls$'
```

Use `openbao.org` for the legacy source contract. Record exact operator sources, images, CRDs, Kubernetes version, and
test output. The four fixtures do not qualify active blue/green transitions or a revisioned pool after an upgrade unless
that fixture actually exercises them. After a failure, inspect the remaining fixture state before running the test again.

## Nonempty restore qualification

`TestOperatorNonemptyRestore` reads a caller-provisioned restore through a loopback `kubectl proxy`. It performs only
the declared target GET, an unfiltered namespace restore LIST, and a final target GET. It verifies exact request and
target UIDs, the selected API contract, the reported phase, finding severity, coverage, and serialized replay. It never
creates, retries, acknowledges, or deletes a restore.

1. Provision fixtures and record their context, namespace, target/request names, and UIDs. Start the proxy with its
   explicit kubeconfig and context. Verify the proxy target before running the test; the declared context identifies
   evidence origin and does not configure the proxy.
2. Set every declared value and expected result. For confirmed disposable retention:

   ```sh
   OPENBAO_CLIENT_RESTORES_ENDPOINT=http://127.0.0.1:18161 \
   OPENBAO_CLIENT_RESTORES_CONTEXT="$context" \
   OPENBAO_CLIENT_RESTORES_NAMESPACE="$namespace" \
   OPENBAO_CLIENT_RESTORES_GROUP=kubebao.org \
   OPENBAO_CLIENT_RESTORES_TARGET="$target" \
   OPENBAO_CLIENT_RESTORES_TARGET_UID="$target_uid" \
   OPENBAO_CLIENT_RESTORES_REQUEST="$request" \
   OPENBAO_CLIENT_RESTORES_REQUEST_UID="$request_uid" \
   OPENBAO_CLIENT_RESTORES_PHASE=Unknown \
   OPENBAO_CLIENT_RESTORES_FINDING=info \
   OPENBAO_CLIENT_RESTORES_COVERAGE=evaluated \
   GOWORK=off go test -race -count=1 -v ./internal/integration -run '^TestOperatorNonemptyRestore$'
   ```

3. After disposable cleanup, run `TestCompletedDisposableRestore` with the recorded original UIDs, phase `Completed`,
   finding `none`, and coverage `insufficient`. It checks the retained cleanup receipt and absent target. A removed target
   cannot support current target findings or establish recovery. The fixture orchestrator separately verifies original
   PVC and Job absence.

Both tests skip unless `OPENBAO_CLIENT_RESTORES_ENDPOINT` is set. A skip is not runtime qualification. Synthetic semantic
and transport tests run under `make check`. Keep fixture provisioning and credentials outside the shared client.

## Qualification and releases

Qualify both consumers against the exact candidate commit before you create a `v0` tag. Record the evidence in the pull
request description, and summarize it in the release notes for the tag:

- Library commit and version, and the consumer commits that pin it.
- Operator version or commit, image digest, and CRD identity.
- Kubernetes version and environment.
- Commands run and their results, including skipped checks.

A skipped check is not qualification. Update [compatibility](compatibility.md) when a validation level changes.

## Restart qualification

`TestOperatorRestartQualification` mutates only a declared disposable Kind fixture. It requires context
`kind-client-restart-rc1`, namespace `restart-tests`, native `kubebao.org` resources, exact target UIDs, and mutation
opt-in. Prepare an isolated cluster with operator `0.7.0-rc.1`, admission enabled, and these initialized Development
fixtures:

| Name | Voters | Steady read replicas | Upgrade strategy |
| --- | --- | --- | --- |
| `voter` | 1 | Not configured | `RollingUpdate` |
| `read-pool` | 3 | 1 | `RollingUpdate` |
| `zero-read` | 1 | 0 | `RollingUpdate` |
| `blue-green` | 1 | Not configured | `BlueGreen` |

Create a `restart-reader` ServiceAccount with cluster GET/LIST and StatefulSet/Pod LIST permissions, and no PATCH
permission. Start a loopback `kubectl proxy` with the disposable kubeconfig and context supplied explicitly. Do not use
an existing operational environment. Replace each UID below with the corresponding fixture UID:

```sh
OPENBAO_CLIENT_RESTART_ENDPOINT=http://127.0.0.1:18164 \
OPENBAO_CLIENT_RESTART_CONTEXT=kind-client-restart-rc1 \
OPENBAO_CLIENT_RESTART_NAMESPACE=restart-tests \
OPENBAO_CLIENT_RESTART_GROUP=kubebao.org \
OPENBAO_CLIENT_RESTART_ALLOW_MUTATION=true \
OPENBAO_CLIENT_RESTART_FIXTURES='{"voter":"<uid>","read-pool":"<uid>","zero-read":"<uid>","blue-green":"<uid>"}' \
GOWORK=off go test -race -count=1 -timeout 15m ./internal/integration \
  -run '^TestOperatorRestartQualification$' -v
```

The harness checks server dry-run without persistence, RBAC denial, cancellation before transport, failed UID/resource-
version/generation patch tests, and refusal of the Blue/Green fixture before transport. It then requests one restart per
rolling fixture, waits for verified current convergence, and compares original and final owned Pod UIDs. The voter test
discards one accepted response and confirms convergence through reads without resubmitting. It reads no Secrets or
OpenBao endpoints and performs no direct Pod mutations.

Stop the owned proxy and delete the disposable Kind cluster after qualification. Preserve operator, image, chart, CRD,
Kubernetes, client, and consumer identities in the qualification pull request. Pod replacement and ready convergence on
one Kubernetes node do not establish fault-domain availability or Raft quorum safety.

## Hosted checks

The Check workflow runs on pull requests, `main` pushes, and manual dispatch. `CI Required` fails if any prerequisite
fails or is skipped. Other workflows:

- **CodeQL** analyzes Go, Python, and Actions. `CodeQL Required` passes only after SARIF processing completes.
- **Dependency Review** evaluates dependency changes in pull requests.
- **Scorecard** runs on `main` and on a schedule. Its results are advisory.
- **Public consumer** builds the published base on pull requests, and the exact published commit after a `main` push.
  Only the `main` run checks the newly published commit.

No workflow publishes tags, releases, images, or deployments.

## Repository settings

Desired settings are committed in `.github/repository`. The organization controls the effective action allowlist and the
web commit sign-off requirement. Review policy changes, then apply and verify them as an administrator:

```sh
python3 scripts/repository-settings.py --apply
python3 scripts/repository-settings.py --check
```

The script changes only this repository and fails on drift. It checks that every pinned workflow action is in the
inherited allowlist. CI never applies settings.

| Control | Configuration |
| --- | --- |
| Default branch | No deletion or force pushes; pull requests; linear history; squash merges; resolved review threads |
| Required checks | `CI Required`, `CodeQL Required`, and Dependency Review from GitHub Actions; `DCO` from the DCO App |
| Review | One approval, CODEOWNERS review, stale-approval dismissal, and approval of the latest push |
| Maintainer exception | `dc-tec` (user ID `63876068`), through pull requests only; cannot bypass required checks |
| Version tags | `v*` tags cannot be updated, deleted, or force-pushed; no bypass actors |
| Merge | Squash only, using the PR title and commit messages so sign-off trailers are kept |
| Actions | Selected actions only; full SHA pins; read-only default token; no PR approval permission |
| Security | Dependency alerts and updates, private vulnerability reports, secret scanning, and push protection |

The installed DCO App (`dco-2`, integration ID `974774`) provides the `DCO` check. Do not add a workflow check with the
same name.
