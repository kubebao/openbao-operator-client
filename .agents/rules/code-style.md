# Go style and naming

Use concrete types and typed enums. Keep dynamic JSON at integration boundaries. Use idiomatic acronym casing such as
`APIGroup`, `UID`, and `ResourceVersion`. Avoid generic `util`, `common`, or `shared` packages. Keep functions focused.
Use the CLI-derived formatters and lint configuration. Run `make lint-fix` and `make check`; do not add blanket exclusions.
