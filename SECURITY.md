# Security

The module is experimental. No operator compatibility range or lifecycle operation is qualified.

Report suspected vulnerabilities through
[GitHub private vulnerability reporting](https://github.com/kubebao/openbao-operator-client/security/advisories/new).
Do not include credentials, kubeconfigs, raw resources, support bundles, or sensitive logs in public issues.

## Boundaries

Evidence and diagnosis contain no Kubernetes clients or credential configuration. Planned adapters project approved
public fields before serialization. Provider messages are excluded by default. Optional local diagnostic text requires
a separate bounded projection policy; redaction does not guarantee that free-form text contains no sensitive data.

Planned observation collectors remain read-only and do not read Secrets, execute commands in Pods, query OpenBao, or
change resources. Caller-declared scopes constrain collection. Failed or partial reads do not establish absence or
resolve previous findings. Names and labels do not establish ownership.

Lifecycle operations and bundle processing are not implemented. Their implementation must add identity, concurrency,
interruption, ambiguous-submission, and untrusted-input checks before exposing them to consumers.

## Checks

Local checks include Go security linting, race tests, architecture enforcement, and `govulncheck`. Hosted checks include
CodeQL for Go, Python, and GitHub Actions; dependency review; and advisory OpenSSF Scorecard reporting. All external
Actions use full commit SHAs. Repository settings require SHA pinning and a read-only default workflow token.
