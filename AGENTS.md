# Engineering rules

Write clear technical English for developers and operators. Follow the Google developer documentation style guide.
Use selected Simplified Technical English principles when they improve procedures. Do not claim formal compliance.
Follow the detailed rules in [.agents/rules](.agents/rules).

## Boundaries

- This module shares evidence, public API projections, bounded collection, and pure diagnostics between clients.
- The operator owns reconciliation and lifecycle safety. Do not import operator or consumer implementation packages.
- `evidence` uses only the Go standard library. `diagnose` uses evidence and pure standard-library code.
- Keep Kubernetes clients, network calls, credentials, and product configuration out of evidence and diagnosis.
- Adapters validate approved public fields before creating evidence. Keep reported facts separate from interpretation.
- Collection accepts caller-configured clients, declared scopes, context cancellation, and bounded limits.
- Missing, invalid, stale, unsupported, denied, and uncollected evidence remain distinguishable.
- Confirm associations through UIDs and documented metadata. Names, labels, and creation times identify candidates only.
- Keep collection time, producer observation time, condition transition time, and analysis reference time separate.
- Exclude provider messages by default. Optional local text needs a separate bounded projection policy before serialization.
- Keep kubeconfig loading, enrollment, credential renewal, scheduling, persistence, rendering, and process results in consumers.
- Keep observation read-only. Add operations and bundle packages only with their first implemented feature.
- Document implemented behavior only. Track planned work in issues. Fixtures and discovery do not qualify an operator.

## Code and verification

- Use concrete types, bounded enums, and narrow interfaces owned by their consumer. Avoid speculative frameworks.
- Allow dynamic JSON only at integration boundaries. Validate external input before core interpretation.
- Handle errors with context. Never log credentials, raw resources, raw provider errors, or Secret data.
- Pass evaluation policy and reference time as inputs. Keep results deterministic for identical evidence and policy.
- Use the pinned tools in Make and the lint settings in `.golangci.yml`.
- Run `make check` before handing back code changes. Run `make vulncheck` after dependency changes.
- Regenerate dependency notices with `make licenses` and verify them with `make check-licenses`.
- Test evidence semantics, incomplete reads, identity conflicts, collection limits, and sensitive-data exclusion.

## Repository workflow

- After the initial bootstrap, use pull requests and squash merges. Follow the configured rulesets.
- Use scoped Conventional Commits and conventional branches such as `feat/evidence-contract`.
- Sign off commits under the Developer Certificate of Origin. Preserve the configured commit signing.
- Preserve consumer repositories and existing environments. Use isolated clusters for integration checks.
- Review generated changes with their source. Never edit generated dependency notices by hand.
- Use immutable `v0` tags. Qualify consumers against the exact candidate before tagging it.
- Record qualification evidence in the pull request and the tag's release notes. Skipped checks are not qualification.
- Update `docs/compatibility.md` when a validation level changes. Do not add status or history records to `docs/`.
