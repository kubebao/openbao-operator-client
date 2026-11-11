# Executable contract specifications

These fixtures define milestone 1 decisions. Value, condition, identity-validation, freshness, restore association/selection,
workload ownership, collection, finding identity, coverage, and representative replay checks call production code.
The remaining reference models describe consumer history, mappings, or unimplemented behavior.
JSON fixtures contain hand-authored expected results. Tests also check input permutations, serialized replay, independent
generation acknowledgement, and history sequences. No Kubernetes dependency is needed to run them.
Collection transcripts express elapsed time and duration limits in seconds and synthetic page sizes in bytes. Their
transport bridge pads wire responses at a fixed scale and maps deadline cases to typed transport errors. Separate HTTP
tests exercise actual context deadlines. Negative byte declarations map to malformed wire data.
Replay bridges the draft fixture envelope to typed cluster captures, supplies read provenance and a reported zero backup
failure count, then calls the production analyzer. JSON decoding in that bridge is test code, not an untrusted bundle parser.

`baseline/` contains additional tests for the immutable consumer commits recorded in `docs/contract.md`. Run them with
`scripts/check-baselines.py`. That command archives each commit into a temporary directory and leaves the checkouts
unchanged. Existing consumer package tests run with the added assertions. Historical behavior remains green even when
it differs from the chosen contract; the documentation identifies each intentional correction.

During milestone 3, replace reference-model calls with production calls one slice at a time. Keep the fixture inputs and
expected results unless a documented source or reference-model correction changes them. Native recovery is unconfirmed under the
pinned contract because its public API is absent. Remote-index cases remain future specifications. Remove reference
models when the supported production implementation covers them. Consumer snapshots remain
historical evidence and are not a CI dependency. Neither suite establishes operator integration qualification.
