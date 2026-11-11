# Verification

Use synthetic fixtures with expected results for semantic decisions. Test missing and malformed fields, independent
resource/condition generations, identity conflicts, partial reads, collection bounds, replacement, and sensitive canaries.
Keep replay deterministic for fixed evidence, policy, and reference time. Test actual consumer request scope.
Use a real API server when claiming Kubernetes validation, defaulting, resource-version, or status-subresource behavior.
Record operator, image, and CRD identities for integration qualification in the pull request. Skipped checks and fixtures
do not qualify an operator.
Run `make check`, plus vulnerability and dependency-license checks for dependency changes.
