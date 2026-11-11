// Package diagnose interprets normalized evidence with caller-supplied policy and
// reference time. It implements finding identity, coverage, cluster conditions,
// restrictions, controller errors, readiness, backup concerns, recorded upgrade
// failures, and restore-test concerns without reading a clock, network, or product state.
package diagnose
