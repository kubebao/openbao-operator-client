# Errors

Check errors before dependent work. Add local context with `%w` when propagating an error across a package boundary.
Use typed Kubernetes errors rather than string matching. Expose sentinels only for caller control flow. Do not panic on
external input. Classify API errors before returning serializable diagnostics; raw response bodies can contain sensitive
values. Keep error classification separate from process exit codes.
