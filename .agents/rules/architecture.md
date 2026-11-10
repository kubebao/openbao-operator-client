# Package boundaries

Use the dependency direction in [architecture](../../docs/architecture.md). Run `make architecture` after changing
imports. Keep `evidence` standard-library-only and diagnosis pure. Adapters own external JSON validation; collection
owns reads. Do not import operator, CLI, or console implementation packages. Add feature packages when needed, not as
speculative scaffolding. Keep interfaces narrow and owned by their consumers.
