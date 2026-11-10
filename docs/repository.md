# Repository configuration

The repository is public at `kubebao/openbao-operator-client`. Desired settings are committed in `.github/repository`.
After the initial branch exists, an administrator applies and verifies them with:

```sh
python3 scripts/repository-settings.py --apply
python3 scripts/repository-settings.py --check
```

The script targets only this repository. It does not change organization policy or reference repositories. Review policy
changes before applying them; CI never applies administrator settings. Verification reads GitHub state and fails on drift.

## Applicable operator controls

| Control | Library configuration |
| --- | --- |
| Default branch | No deletion or force pushes; pull requests; linear history; squash merges; resolved review threads |
| Required checks | CI Required, CodeQL Required, DCO, and Dependency Review; bound to GitHub Actions |
| Review approval | One approval, CODEOWNERS review, stale-approval dismissal, and approval of the latest push |
| Maintainer review exception | `dc-tec`, user ID `63876068`, through pull requests only, in the separate review ruleset |
| Version tags | Prevent update, deletion, and force pushes for `v*`; no bypass actors |
| Merge behavior | Squash only; use PR title and commit messages so sign-off trailers are retained |
| Actions | Selected actions only; full SHA pinning required; read-only default token; no PR approval permission |
| Security | Dependency alerts and updates, private vulnerability reports, secret scanning, and push protection |

The review exception matches the operator's layered policy. It cannot bypass the default branch's required checks or
permit direct pushes. No release-app exception is copied because this library has no release automation. Authorized
maintainers can create new `v0` tags after qualification; existing tags remain immutable.

The library implements DCO validation in its own workflow instead of requiring an unverified external GitHub App
installation. The rule requires the workflow's `DCO` check from GitHub Actions. A valid sign-off trailer must name the
commit author. Cryptographic commit signing remains enabled in the maintainer's local Git configuration.

Action allowlist entries name the used action paths; repository SHA-pinning and pinned workflow references constrain
their versions. CodeQL and Scorecard upload actions are included so selected-action policy does not prevent scans.

The operator's release-PR gate, release branches, Pages branch, artifact publishing, deployment environments, and
release-app-only tag creation do not apply to this initial library. No release tag is created by scaffolding.
