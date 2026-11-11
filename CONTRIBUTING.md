# Contributing

Read [AGENTS.md](AGENTS.md) and the detailed [.agents/rules](.agents/rules) before changing code.
Use Go 1.27.4 or a newer compatible toolchain, Make, Bash, and Python 3.

```sh
make install
make check
```

Run `make lint-fix` to apply the configured formatting and supported lint fixes. Run `make vulncheck` after dependency
changes. Regenerate dependency notices with `make licenses`, then run `make check-licenses`.

Use focused pull requests with Conventional Commit titles, such as `feat(evidence): define collection outcomes`.
Use conventional branch names such as `feat/evidence-contract`. Sign off each commit with `git commit -s` under the
[Developer Certificate of Origin](https://developercertificate.org/). Sign-off and cryptographic signing are separate.

After the bootstrap commit, the default branch requires pull requests, passing checks, linear history, and resolved
review threads. Squash merge is the permitted merge method. A separate ruleset requires owner review and approval of
the latest push. Its maintainer exception matches the operator repository and applies only through pull requests;
it does not bypass required CI, DCO, or CodeQL checks. See [repository settings](docs/development.md#repository-settings).

Describe the resulting behavior, material compatibility or security implications, and meaningful checks. Update the
reference docs in the same pull request as the behavior. Add synthetic
fixtures and expected results for semantic changes. Do not preserve a defect solely because a consumer is unreleased.
Version changed contracts and document the treatment of existing stored data.

The library must remain independent of console and CLI implementation packages. Consumer migrations use the same
candidate commit and final tag. Record qualification evidence in the pull request, as described in
[qualification and releases](docs/development.md#qualification-and-releases).

Report security vulnerabilities through the private channel in [SECURITY.md](SECURITY.md).
