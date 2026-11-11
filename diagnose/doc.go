// Package diagnose interprets normalized evidence with caller-supplied policy and
// reference time. It implements finding identity, coverage, replica readiness,
// and backup completion age without reading a clock, network, or product state.
package diagnose
