# Documentation

Follow the OpenBao Operator
[documentation standard](https://github.com/kubebao/openbao-operator/blob/main/website/EDITORIAL.md). It applies the
Google developer documentation style guide. Use selected Simplified Technical English principles when they improve
procedures. Do not claim formal compliance with either.

## Write directly

- Write for the reader's task. State the outcome before background or rationale.
- Use active voice, second person, present tense, and short, direct sentences. Use exact API and command names.
- Use sentence case for headings. Start task headings with a base-form verb. Use noun phrases for concept and reference
  headings.
- Use `must` for requirements, `can` for capability, and `might` for possibility.
- Put prerequisites before a procedure. Number ordered steps and keep one action in each step. Explain placeholders such
  as `<namespace>` before the command, and state the observable result after it.
- Use a warning callout only when ignoring it can cause material harm. Use a note for scope, ownership, and limitations.
- Use relative links, descriptive link text, and language-tagged code blocks. Wrap prose at 120 characters.
- Define uncommon terms. State conditions before dependent actions, and include verification results.
- Follow the project's terminology when it differs from general guidance.
- Avoid filler, marketing claims, idioms, and repeated explanations. Simplify language without removing technical
  detail.

## Document current behavior

- Describe current behavior and support boundaries. Update documentation in the same change as the behavior.
- Do not add roadmaps, plans, milestones, audits, migration notes, or status records to the repository. Track planned
  work in issues. Git history, pull requests, and release notes record changes and qualification evidence.
- Do not describe earlier behavior or corrections unless readers need that information to upgrade.
- Record validation levels and unsupported capabilities in `docs/compatibility.md`. Do not present fixture or synthetic
  checks as operator qualification.
- Put API details in Go doc comments.
- Keep security, data, availability, access, and compatibility boundaries explicit when you shorten a page.
