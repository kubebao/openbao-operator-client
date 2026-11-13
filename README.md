# OpenBao Operator client

Shared Go library for the [OpenBao Operator](https://github.com/kubebao/openbao-operator) console and CLI. It reads
OpenBao Operator and Kubernetes resources, projects approved fields into typed evidence, and evaluates diagnostic rules
on that evidence. A separate operations client submits guarded pause and maintenance requests.

| Package | Responsibility |
| --- | --- |
| `evidence` | Typed observations, identities, value states, times, collection outcomes, and UID associations. Standard library only. |
| `adapter` | Projection of approved public API fields from JSON under an explicit contract. |
| `collect` | Bounded discovery, GET, and LIST reads through a caller-configured HTTP transport. |
| `diagnose` | Pure rule evaluation with caller-supplied policy and reference time. Returns findings and coverage. |
| `operations` | Plans and submits pause and maintenance requests, then assesses supplied observations. |

The operator owns reconciliation and lifecycle safety. The console and CLI own credentials, kubeconfig loading,
scheduling, persistence, rendering, and process exit codes.

## Documentation

- [Architecture](docs/architecture.md): package boundaries and what belongs in consumers.
- [Evidence semantics](docs/semantics.md): value states, projection, identity, time, associations, and collection.
- [Diagnostics](docs/diagnostics.md): policy, coverage, findings, and the rule catalog.
- [Operations](docs/operations.md): request previews, concurrency guards, submission outcomes, and observation.
- [Compatibility](docs/compatibility.md): supported contracts, the Kubernetes version floor, and validation levels.
- [Development](docs/development.md): checks, consumer development, and repository settings.

## Development

Use Go 1.27.4 or later, Make, Bash, and Python 3.

```sh
make check
go run ./examples/diagnose
```

The example projects a synthetic cluster and prints `2 findings, 15 coverage entries`. It makes no network requests.

## Versioning

The module path is `github.com/kubebao/openbao-operator-client`. Releases use immutable `v0` tags, and the API can change
between them. Consumers pin an immutable release tag or a Go pseudo-version of a published commit.

Each release defines one evidence model and diagnostic implementation. Evidence and policy carry no separate schema
or rule-set version. The client provides no compatibility readers for earlier serialized models. Consumers own their
stored formats and must reject incompatible input before analysis.

The project uses the [Apache License 2.0](LICENSE). [NOTICE](NOTICE) records scaffold provenance, and
[THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) records dependency notices.
