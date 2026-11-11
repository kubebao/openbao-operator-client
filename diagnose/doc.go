// Package diagnose interprets normalized evidence with caller-supplied policy and
// reference time. It implements finding identity, coverage, cluster conditions,
// restrictions, controller errors, readiness, and backup age without reading a
// clock, network, or product state.
package diagnose
