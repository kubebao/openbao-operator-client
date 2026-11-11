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

## Qualification and releases

Qualify both consumers against the exact candidate commit before you create a `v0` tag. Record the evidence in the pull
request description, and summarize it in the release notes for the tag:

- Library commit and version, and the consumer commits that pin it.
- Operator version or commit, image digest, and CRD identity.
- Kubernetes version and environment.
- Commands run and their results, including skipped checks.

A skipped check is not qualification. Update [compatibility](compatibility.md) when a validation level changes.

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
