# Generated artifacts and dependencies

Update inputs and regenerate outputs with pinned tools. Review generated diffs. Run `make licenses` after dependency
changes and `make check-licenses` to verify notices. Do not copy the operator's vendor, CRD, Helm, or controller generators
into this library. Match the consumers' Go and Kubernetes versions when dependencies are introduced. Keep local workspace
and replacement directives out of independent builds. Tags are immutable; record candidate and consumer commits in the tag's release notes.
