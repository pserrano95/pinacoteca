# Pinacoteca — Roadmap

> Milestones, not sessions. Each milestone has an executable **"done when"**
> and links its issues: **status is the issues on GitHub**; nothing here says
> "built". Milestones without an issue are written up as a task **when their
> turn comes**, with the repository in front of us.
>
> The first is the minimal vertical slice. After it, batches: issues in a
> batch do not share files and run at the same time; the next batch starts
> when the previous one is merged.

## Up to the MVP

### H0 — Skeleton, CI and the first vertical slice

**Done when:** CI is green on `main` (tests, lint, container build), and on a
fresh instance one can create an artist, upload a photo of a drawing with its
metadata, see it in the gallery, find it on disk as `original.*` +
`artwork.json` (with `schema_version`) and an empty `derived/`, and a test
proves that deleting the database and rebuilding the index from disk
reproduces the same gallery (D-004). No users or login yet. Issue: to be
published.

### H1 — Members, galleries and invitations

**Done when:** an owner creates a gallery, adds artists to it and invites
relatives by link; they join with a passkey, with the roles owner,
contributor and viewer enforced and tested (D-007, D-009).

**Depends on:** H0.

### H2 — Guardians and export

**Done when:** each artist has guardians who approve the galleries the child
appears in, can hide or delete any of the child's works, and can download all
of them as a folder that reads without Pinacoteca (D-008, D-004).

**Depends on:** H1.

### H3 — Capture from the phone

**Done when:** the app installs on a phone home screen; a drawing is
photographed, cropped and straightened in the browser, and uploads queued
while offline are sent when the connection returns.

**Depends on:** H0.

### H4 — The exhibition

**Done when:** the gallery looks like an exhibition, not a file list: a
masonry layout, an artwork viewer with swipe and pinch-zoom, smooth
transitions between pages, and the whole interface in Spanish and English.
Reviewed against the visual direction agreed in Q2.

**Depends on:** H1, H3.

### H5 — Installing a family instance

**Done when:** a documented, copy-paste path takes a small VPS to a running
instance over HTTPS with automatic updates and a backup recipe, and the
founding family's instance runs on it.

**Depends on:** H2, H4.

## MVP

**Done when:** the founding family's instance is live over HTTPS; at least
two relatives other than the host have joined by invitation; drawings have
been captured from a phone; a guardian has approved a gallery; and an export
of one artist opens correctly without Pinacoteca. At that point the project
leaves the construction phase.

## Later

- AI scene description and tag suggestions (D-010).
- Character animation with an optional heavy-model container (D-010).
- A timeline of each artist's drawings by age.
- A yearly book as a printable PDF.
- A Svelte front end, only if D-006's revisit condition is met.
