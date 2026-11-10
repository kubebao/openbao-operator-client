# Security boundaries

Project approved fields before persistence, temporary files, output, or logs. Exclude provider messages by default.
Optional local text needs a separate bounded policy. Never read Secrets, execute Pod commands, or mutate resources in
observation collection. Preserve caller transport and credential policy. Names and labels do not prove ownership.
Require UID confirmation and retain association basis; reject conflicting identity. Bound duration, pages, candidates,
and retained data. Use ordinary admission and optimistic concurrency for future operations; never bypass safeguards.
