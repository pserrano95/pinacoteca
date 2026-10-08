# AGENTS.md — Pinacoteca

## How to work here

If your first message starts with `[orca:`, nobody is there to answer you:
**read `ORCA.md` before anything else** and follow it as well as this file.

Otherwise a person is working with you. Work with them: ask before guessing,
and do not open branches, pull requests or issues they did not ask for.

In both cases, **never** merge a pull request, push to `main` or rewrite
published history unless the person in front of you explicitly asks for it in
this conversation. With `[orca:`, never.

## What this is

A private, self-hosted art gallery for the drawings children give to their
family. Read `CONTEXT.md` before changing behaviour, and `docs/DECISIONS.md`
before changing structure: both are binding.

## The business

None, on purpose (`CONTEXT.md`). Open source (AGPL-3.0), one instance per
family (D-001). Do not add anything that only makes sense for a hosted
service, analytics or monetisation.

## Commands

None yet: the repository has no code. The H0 milestone creates the Go module,
the `Makefile` and the CI, and must fill in this section and «What CI checks»
with them.

## Hard constraints

- Code, docs, commits and pull requests in English; every user-facing string
  goes through `es` and `en` translation files, none hard-coded (D-003).
- The archive is the filesystem and the database is a rebuildable index:
  originals are immutable, everything generated lives in `derived/`,
  `artwork.json` carries `schema_version`, and rebuilding the index from disk
  must reproduce the same gallery (D-004).
- One binary in one container, Go and SQLite without cgo, no external services
  (D-005). Server-rendered HTML with JavaScript islands, no front-end build
  step (D-006).
- Nothing about a child leaves the instance without a guardian's explicit,
  per-artist consent; AI is optional and off by default (D-010).
- Decisions in `docs/DECISIONS.md` are not reopened in passing: a change of
  direction is a new decision, recorded there.

## What is not touched

- `CONTEXT.md` and accepted decisions, except through a recorded decision.
- `LICENSE`.

## What CI checks

Nothing yet (see «Commands»). When it exists, a skipped step counts as red.

## Commits

In English, saying what changes and **why**, not only what.

If you find that something in this file is no longer true, say so — to the
person you are working with or, with nobody there, in the pull request.
