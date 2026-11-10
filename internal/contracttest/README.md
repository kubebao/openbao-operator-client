# Executable contract specifications

These fixtures define milestone 1 decisions. Value, condition, identity-validation, and freshness checks call production
code. The remaining reference models are test-only and do not implement production collectors or diagnostic rules.
JSON fixtures contain hand-authored expected results. Tests also check input permutations, serialized replay, independent
generation acknowledgement, and history sequences. No Kubernetes dependency is needed to run them.
Collection transcripts express elapsed time and duration limits in seconds and retained sizes in bytes.

`baseline/` contains additional tests for the immutable consumer commits recorded in `docs/contract.md`. Run them with
`scripts/check-baselines.py`. That command archives each commit into a temporary directory and leaves the checkouts
unchanged. Existing consumer package tests run with the added assertions. Historical behavior remains green even when
it differs from the chosen contract; the documentation identifies each intentional correction.

During milestone 3, replace reference-model calls with production calls one slice at a time. Keep the fixture inputs and
expected results unless a documented source correction changes the contract. Native recovery is unconfirmed under the
pinned contract because its public API is absent. Remote-index cases remain future specifications. Remove reference
models when the supported production implementation covers them. Consumer snapshots remain
historical evidence and are not a CI dependency. Neither suite establishes operator integration qualification.
