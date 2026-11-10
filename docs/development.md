# Development

Use Go 1.27.3 or a newer compatible toolchain, Make, Bash, and Python 3. GitHub administration also requires an
authenticated `gh` with repository administration access. Go selects the required toolchain with `GOTOOLCHAIN=auto`.

| Command | Verification |
| --- | --- |
| `make check` | Lint, race tests, package/example build, architecture boundaries, tooling tests, and workflow validation |
| `make check-modules` | Module manifests match `go mod tidy` |
| `make vulncheck` | Reachable Go and standard-library vulnerabilities |
| `make licenses` | Generate notices for library/example dependencies and the Go runtime |
| `make check-licenses` | Generated notice drift |
| `make consumer REVISION=<published-commit>` | Unauthenticated external-module build from fresh caches, with no workspace or replacement |
| `python3 scripts/repository-settings.py --check` | Live GitHub configuration matches committed policy |

Pinned tools are golangci-lint `v2.14.0`, govulncheck `v1.9.0`, go-licenses `v2.0.1`, and actionlint `v1.7.12`.
Formatting follows the CLI: gofumpt, goimports, and golines with a 140-character line limit. The scaffold has no Kubernetes
dependency yet. Introduce Kubernetes `v0.37.2`, matching the baseline consumers, with the first adapter/collector slice.

## Coordinated consumer development

Create a workspace outside the three repositories or use an untracked workspace file. For sibling checkouts:

```sh
go work init ./openbao-operator-client ./openbao-operator-console ./openbao-operator-cli
```

Workspace builds are development feedback. Final builds use a pinned version with `GOWORK=off` and no local replacements.
This public module does not need `GOPRIVATE` or download credentials. Avoid matching it with a broader private prefix
when testing public proxy/checksum resolution. The external-consumer script disables Go authentication, ignores system
and global Git configuration, clears GitHub/Git/SSH environment variables, and uses the public Go proxy without direct
Git fallback. It uses fresh module/build caches and reuses the
selected Go toolchain and removes its temporary writable module cache after verification.

## Hosted checks

Check runs on pull requests, `main` pushes, and manual dispatch. CI Required fails if any prerequisite fails or skips.
CodeQL analyzes Go, Python, and Actions and verifies that SARIF processing completes before CodeQL Required passes.
Dependency Review evaluates pull-request dependency changes. Scorecard runs on `main` and a schedule as advisory evidence.
Public consumer runs after publication to `main`; its external download cannot run against unpublished local code.

No workflow publishes release tags, releases, images, or deployments. Cross-compilation checks package build coverage,
not runtime or operator compatibility. No credentials are needed to fetch the library from its public repository.

## Reference sources

Scaffolding follows CLI commit `adbe0f42fac62bdc6dd2d7995a8aecb36102e9a3`. Engineering rules and applicable governance follow
operator commit `9671c30623d609f948cf073e167f376de4ea9a85` and its live repository policies inspected on 10 November 2026.
Controller architecture, CRD/Helm generators, release-only checks, website tooling, and binary distribution do not apply
to this module scaffold.
