# OpenBao Operator client

Shared Go library for the [OpenBao Operator](https://github.com/kubebao/openbao-operator) console and CLI.

This project is in initial development. It implements typed cluster and workload evidence, generation acknowledgement,
timestamps, collection outcomes, operational observations, UID associations, and approved operator/Kubernetes JSON projection.
Native recovery and remote replication are unsupported by the pinned operator API. Bounded discovery, GET/LIST collection,
and declared related-resource collection with final target verification are implemented. Diagnosis implements fifteen
rules for clusters, workloads, backups, recorded upgrade failures, restore tests, and snapshot-restore concerns.
Neither application uses the module yet. No operator capability is integration-qualified.
The [extraction status](docs/extraction.md) records the implemented milestone 3 slices and their qualification limits.

## Packages

| Package | Responsibility | Implementation |
| --- | --- | --- |
| `evidence` | Typed observations, identities, outcomes, times, associations, and references | Cluster/workload facts, field states, references, times, outcomes, associations |
| `adapter` | Approved public API projections and capability definitions | Cluster, restore, StatefulSet, Pod projection; UID associations; explicit contracts |
| `collect` | Bounded discovery, resource reads, pagination, and identity verification | Discovery, scoped GET/LIST, shared budgets, declared related reads, target verification |
| `diagnose` | Pure rules, findings, coverage, and evaluation policy | Fifteen supported rules, finding identity, coverage, deterministic analysis |

The operator owns reconciliation and lifecycle safety. Consumer applications own credentials, enrollment, scheduling,
persistence, rendering, and process exit codes. Operations and bundle handling get separate packages when implemented.

## Development

Use Go 1.27.4 or a newer compatible toolchain, Make, Bash, and Python 3. Go downloads the pinned development tools.

```sh
make install
make check
make vulncheck
make check-licenses
go run ./examples/generation
go run ./examples/cluster
go run ./examples/diagnose
```

The generation example prints `stale`. The cluster example prints current acknowledgement and an implied zero ready
count. The diagnostic example prints `2 findings, 15 coverage entries`. These examples use synthetic input and make no
network requests. The output does not establish application health.

See the [contract decisions](docs/contract.md), [extraction plan](docs/plan.md),
[development guide](docs/development.md), and [compatibility matrix](docs/compatibility.md).

## Versioning and license

The module path is `github.com/kubebao/openbao-operator-client`. Initial releases use immutable `v0` tags. The API is
experimental and can change between versions. No release tag exists yet. Consumers can use Go-generated pseudo-versions
for published development commits. Final extraction qualification requires both consumers to pin the same tag.

The project uses the [Apache License 2.0](LICENSE). [NOTICE](NOTICE) records scaffold provenance.
[THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) records dependency notices.
