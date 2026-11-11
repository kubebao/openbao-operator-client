# Contract tests

These tests check evidence and diagnostic semantics against synthetic fixtures in `testdata/`. Each fixture holds named
scenarios with hand-authored inputs and expected results. Every test calls production code.

| Area | Fixtures |
| --- | --- |
| Value states, omission rules, and conditions | `values.json`, `conditions.json` |
| Projection boundary and provider-text exclusion | `projection.json` |
| Time and freshness | `time.json` |
| Restore associations and display selection | `associations.json`, `selection.json` |
| Workload ownership chains | `ownership.json` |
| Collection outcomes and limits | `collection.json` |
| Coverage resolution | `coverage.json` |
| Finding identity and order | `findings.json` |
| Serialized replay through `diagnose.Analyze` | `replay.json` |

The fixtures use a simplified input envelope. Test helpers map it to production inputs: they supply read provenance, and
replay supplies a reported zero backup-failure count. Collection fixtures express time in seconds and page sizes in bytes.
A synthetic transport pads responses at a fixed scale and maps deadline cases to transport deadline errors. HTTP tests in
`collect` exercise real context deadlines.

Change an expected result only together with the behavior it specifies, and record the reason in the pull request.
