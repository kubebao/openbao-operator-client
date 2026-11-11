// Package client documents the shared OpenBao Operator client module.
//
// The evidence, adapter, collect, and diagnose packages separate observations,
// public API projection, Kubernetes reads, and interpretation. See the package
// documentation and docs/semantics.md for evidence semantics. The separate
// operations package prepares guarded control requests and assesses supplied
// observations; observation collection remains read-only.
package client
