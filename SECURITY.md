# Security

The module is experimental. Validation covers only the operator builds and fixtures listed in
[compatibility](docs/compatibility.md). No operator compatibility range is qualified.

Report suspected vulnerabilities through
[GitHub private vulnerability reporting](https://github.com/kubebao/openbao-operator-client/security/advisories/new).
Do not include credentials, kubeconfigs, raw resources, support bundles, or sensitive logs in public issues.

## Boundaries

Evidence and diagnosis contain no Kubernetes clients or credential configuration. Adapters project approved public
fields before serialization. Provider messages are excluded by default. Optional local diagnostic text requires a
separate bounded projection policy; redaction does not guarantee that free-form text contains no sensitive data.

Collectors are read-only. They do not read Secrets, execute commands in Pods, query OpenBao, or change resources.
Caller-declared scopes constrain collection. Failed or partial reads do not establish absence or resolve previous
findings. Names and labels do not establish ownership.

The `operations` package uses a separate write client. Each request changes one field and is bound to the selected
UID, resource version, and generation. A request with an ambiguous outcome is not resubmitted. See
[operations](docs/operations.md). The client does not process support bundles; consumers own archive handling.

## Checks

Local checks include Go security linting, race tests, architecture enforcement, and `govulncheck`. Hosted checks include
CodeQL for Go, Python, and GitHub Actions; dependency review; and advisory OpenSSF Scorecard reporting. All external
Actions use full commit SHAs. Repository settings require SHA pinning and a read-only default workflow token.
