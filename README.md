# OpenBao Operator client

Shared Go library for the [OpenBao Operator](https://github.com/kubebao/openbao-operator) console and CLI.

This project is in initial development. It implements generation acknowledgement primitives and synthetic contract
fixtures. API adapters, live collection, and diagnostic rules are planned. Neither application uses the module yet.
No operator version or capability is integration-qualified. The [contract specifications and consumer baselines](docs/baselines.md)
record milestone 1 decisions before production extraction.

## Packages

| Package | Responsibility | Implementation |
| --- | --- | --- |
| `evidence` | Typed observations, identities, outcomes, times, associations, and references | Generation acknowledgement only |
| `adapter` | Approved public API projections and capability definitions | Package boundary only |
| `collect` | Bounded discovery, resource reads, pagination, and identity verification | Package boundary only |
| `diagnose` | Pure rules, findings, coverage, and evaluation policy | Package boundary only |

The operator owns reconciliation and lifecycle safety. Consumer applications own credentials, enrollment, scheduling,
persistence, rendering, and process exit codes. Operations and bundle handling get separate packages when implemented.

## Development

Use Go 1.27.3 or a newer compatible toolchain, Make, Bash, and Python 3. Go downloads the pinned development tools.

```sh
make install
make check
make vulncheck
make check-licenses
go run ./examples/generation
```

The example prints `stale`. It uses synthetic generation values and makes no network requests.

See the [contract decisions](docs/contract.md), [extraction plan](docs/plan.md),
[development guide](docs/development.md), and [compatibility matrix](docs/compatibility.md).

## Versioning and license

The module path is `github.com/kubebao/openbao-operator-client`. Initial releases use immutable `v0` tags. The API is
experimental and can change between versions. No release tag exists yet. Consumers can use Go-generated pseudo-versions
for published development commits. Final extraction qualification requires both consumers to pin the same tag.

The project uses the [Apache License 2.0](LICENSE). [NOTICE](NOTICE) records scaffold provenance.
[THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) records dependency notices.
