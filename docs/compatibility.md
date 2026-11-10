# Compatibility and qualification

No operator capability is integration-qualified. Package compilation, synthetic fixtures, and API discovery do not
establish compatibility with an installed operator.

| Operator source | API group/version | Capability | Validation level | Evidence |
| --- | --- | --- | --- | --- |
| Not selected | Not applicable | Generation acknowledgement | Synthetic contract fixtures | `evidence/testdata/generations.json` |
| `9671c30623d609f948cf073e167f376de4ea9a85` | `openbao.org/v1alpha1` | API projection and collection | Source inspected; implementation pending | `docs/contract.md` |

Record each qualified capability against an operator commit or version, installed CRD identity, image digest, library
commit, test command, environment, result, and evidence location. Keep unsupported and untested capabilities identifiable.
Add the migrated API group only when its specific contract and validation evidence are available.

## Consumer collection coverage

Neither consumer imports this module yet. These are migration boundaries from the baseline source, not library qualification.

| Consumer | Declared scope |
| --- | --- |
| Console | Enrolled namespace lists of clusters and optional restore/recovery requests; named enrolled controller Deployment reads |
| CLI `status` | One named cluster and API discovery; subsequent polling remains bound to the selected group and UID |
| CLI `doctor` | Status plus bounded StatefulSet, Pod, and restore candidate reads with identity verification |

Track application scope separately from library capability. Keep one library implementation of each shared projection
and rule without requiring each application to collect every supported evidence type.
