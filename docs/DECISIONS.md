# Pinacoteca — Decision record

> Fixed format per entry: **decision, status, reason, alternatives
> considered, consequences**. Statuses: **proposed** (do not build on it
> without confirming), **accepted**, **rejected**, **superseded**,
> **blocked**. An entry is never deleted: it is marked `superseded` and
> linked to the new one.
>
> **Only what had a rejected alternative goes in here.** Everything else is a
> commit message. Every structural decision names **the simplest alternative
> that was discarded and why**: if we cannot say why the simple option does
> not work here, it does.

---

## D-001 — One self-hosted instance per family, no central service

**Status:** accepted · 2026-10-04

**Reason.** The archive holds images and data of minors. A central service
would make whoever runs it the custodian of many families' children, with the
legal duties that come with it (GDPR consent, deletion, security), and turn a
family project into a service with obligations. With one instance per family,
each family keeps its own.

**Alternatives considered.**
- *A hosted multi-tenant service.* Discarded: custody of other families'
  minors' data, for a product with no business model behind it.
- *Store the drawings in each family's Google Drive or iCloud and keep only an
  index.* Discarded: roles and guardian rights cannot be enforced on files
  that live in someone else's account.

**Consequences.** Installing and running an instance must be easy enough for
a family with modest technical help (see the roadmap). An instance can host
several galleries and invite people from other households. Nothing is
designed for a hosted offering.

---

## D-002 — AGPL-3.0 licence

**Status:** accepted · 2026-10-04

**Reason.** Anyone may use, modify and self-host Pinacoteca, but whoever
offers it as a network service must publish their changes. The copyright
holder keeps the option of offering a hosted version if it ever proves
valuable.

**Alternatives considered.**
- *MIT.* The simplest. Discarded: it lets a third party run a closed hosted
  service on this code without giving anything back.

**Consequences.** Contributions are accepted under the same licence.

---

## D-003 — The repository is written in English; the app is bilingual

**Status:** accepted · 2026-10-04

**Reason.** The project is open source and meant for any family. Code,
README and documentation in English are what let others find it and
contribute. The interface ships in Spanish and English from the start.

**Alternatives considered.**
- *Everything in Spanish.* Simpler for the founding team. Discarded: closes
  the door to almost everyone else.

**Consequences.** No user-facing string is hard-coded; all go through
translation files from the first slice.

---

## D-004 — The archive is the filesystem; the database is a rebuildable index

**Status:** accepted · 2026-10-04

**Reason.** A child's drawings must still be readable in twenty years, longer
than any app is guaranteed to live. Each artwork is a folder with the
original image, an `artwork.json` with its metadata and a `derived/` folder
for anything generated from it. The database only indexes those files and
can be rebuilt from them. Exporting is copying a folder, which is what gives
guardians real control (D-008).

**Alternatives considered.**
- *The database as the source of truth, images as blobs or loose files.* The
  simplest. Discarded: the archive would only be readable through
  Pinacoteca, and export would be a feature to build and keep correct
  instead of a property of the storage.

**Consequences.**
- Originals are **immutable**: never edited, never re-encoded in place.
- Everything generated (thumbnails, crops, AI outputs) lives in `derived/`,
  with a sidecar `.json` saying how it was made. It can be deleted and
  regenerated.
- `artwork.json` carries a `schema_version`.
- A rebuild of the index from disk must reproduce the same gallery. A test
  enforces it from the first slice.

---

## D-005 — A single container: Go and SQLite

**Status:** accepted · 2026-10-04

**Reason.** One self-contained binary in one container runs on a small VPS, a
NAS or a Raspberry Pi, with no other services to install, configure or keep
alive. Fewer moving parts is what makes self-hosting realistic (D-001).

**Alternatives considered.**
- *Serverless on a cloud provider (e.g. Cloudflare Workers, D1, R2).* No
  server to maintain. Discarded: ties every instance to one provider's account
  and formats, against D-001 and D-004.
- *Node or Python with PostgreSQL.* Discarded: a second service to run and
  back up, and a heavier runtime, for no need this project has.

**Consequences.** Reaching an instance from outside the home and keeping it
updated stay with each family; the roadmap covers a documented VPS path.

---

## D-006 — Server-rendered HTML with JavaScript islands

**Status:** accepted · 2026-10-04

**Reason.** Pages are rendered by the Go server (templates and htmx). Plain
JavaScript islands handle what needs it: camera capture and cropping, and the
artwork viewer (swipe, pinch-zoom). One language, one binary, no front-end
build step. How the gallery looks is CSS, not a property of the rendering
model.

**Alternatives considered.**
- *A single-page app (Svelte or React).* Smoother navigation. Discarded for
  now: it doubles the stack, and a gallery does not need it.

**Consequences.** **Revisit if**, when the founding family uses the gallery,
it feels clumsy in a way CSS and islands cannot fix. A Svelte front end can
be embedded in the same binary, so switching does not break D-005.

---

## D-007 — Artworks belong to artists; a gallery is artists plus members

**Status:** accepted · 2026-10-04

**Reason.** A child may need to appear in more than one gallery (one with
each side of the family, one with the cousins). If artworks lived inside a
gallery they would have to be duplicated. Instead each artwork belongs to one
artist, and a gallery is a set of artists plus a set of members, showing all
the works of its artists.

**Alternatives considered.**
- *Artworks stored inside a gallery.* The simplest. Discarded: duplication
  the first time a child is in two galleries.

**Consequences.**
- An artwork records **gifted to**: who received it and who holds the
  original.
- Gallery roles: **owner** (invites, manages), **contributor** (uploads),
  **viewer** (only looks).
- An artist has a birth date, so the age at each artwork is computed, not
  typed.

---

## D-008 — Every artist has guardians

**Status:** accepted · 2026-10-04

**Reason.** Whoever hosts the instance or uploads the drawing, the child's
work should be controlled by the child's parents. A guardian role on the
artist, separate from gallery roles, decides which galleries show the child,
edits the child's data, can hide or delete any of the child's works, and can
export all of them.

**Alternatives considered.**
- *Gallery roles only.* Simpler. Discarded: the owner of a gallery, not the
  parents, would control the child.

**Consequences.** Whoever creates an artist is a provisional guardian until
they invite the parents; if the parents never accept, everything keeps
working. It ships in the first version, at least approving galleries and
deleting: retrofitting it means reassigning control over every existing
child. Known limit: the host has technical access to everything. The real
guarantee is export (D-004).

---

## D-009 — Invite links and passkeys; email is optional

**Status:** accepted · 2026-10-04

**Reason.** Relatives join by opening an invite link and register a passkey
(fingerprint or face on their phone). No passwords to forget, and no mail
server to configure for an instance to work.

**Alternatives considered.**
- *Username and password.* The simplest. Discarded: forgotten passwords with
  no email recovery, on an app used by grandparents.
- *Email magic links as the only way in.* Discarded: forces every family to
  configure outgoing mail.

**Consequences.** Email, when configured, adds notifications and a recovery
path. A lost passkey is recovered by a gallery owner re-inviting.

---

## D-010 — AI is pluggable, optional and off by default per artist

**Status:** accepted · 2026-10-04

**Reason.** AI can describe scenes, suggest tags and, later, animate
characters. All of it is optional: Pinacoteca works fully without it. A
provider is configured per instance (any OpenAI-compatible API, a local model
such as Ollama, or none), and sending a child's drawing to any provider
requires the consent of that child's guardian, off by default.

**Alternatives considered.**
- *A built-in cloud AI provider.* Discarded: sends minors' drawings to a
  third party by default, and puts a bill on every instance.

**Consequences.** AI outputs are derived files (D-004). Heavy models (e.g.
animation) run as an optional extra container or an external API, never as a
requirement of the base install.
