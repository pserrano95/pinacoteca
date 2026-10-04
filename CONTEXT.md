# Pinacoteca — Project context

> What this is and who it is for, without saying how it is built. **It does
> not fix architecture, framework, database or deployment**: that lives in
> `docs/DECISIONS.md`, with its alternatives. And **it does not report
> status**: status is the issues and `docs/ROADMAP.md`. It changes only
> through a recorded decision.

## Summary

Pinacoteca turns the drawings a child gives to their family into a lasting,
private art gallery. Each drawing is photographed, stored with its context
(who drew it, at what age, what it shows, who received it) and exhibited to
the people the child's family chooses. Without it, the drawings end up in a
drawer, in a camera roll mixed with everything else, or in the bin.

## People and users

- **Artist:** the child. Never a user of the app; the subject of the archive.
- **Guardian:** a parent (or whoever is responsible for the child). Decides
  where the child's work is shown, can hide, delete and export all of it.
- **Collector:** a relative who receives drawings (an uncle, a grandmother)
  and wants to keep and show them. Often the person who uploads.
- **Viewer:** a relative who only wants to look.
- **Host:** the person who runs an instance for their family. Technically
  able to read everything stored on it.

## The business

None, on purpose. Pinacoteca is open source (AGPL-3.0) and self-hosted: each
family runs its own instance. The existing products in this space (Artkive,
Keepy, Canvsly; since 2012–2013) are built for the parent who archives their
own child's output and make their money from printed books. Pinacoteca is
built for the family that **receives** the drawings, and asks for nothing in
return. A hosted offering is not ruled out forever, but nothing is designed
for it.

## Objectives

1. A family can keep every drawing a child gives them, with its context, in a
   place that will still be readable in twenty years.
2. Several relatives share the same gallery without anyone having to send
   photos around.
3. Any family with modest technical help can install and run its own
   instance.

## Product principles

### The drawing is the work, not a photo of it

Every screen treats a drawing as an artwork in an exhibition: title, artist,
age, date, story. Never as a file in a list.

### The archive outlives the app

The originals and their metadata are stored as plain files that anyone can
read without Pinacoteca. If the project dies, the archive does not.

### The child belongs to their guardians

Whoever hosts or uploads, the child's guardians decide where the child's work
appears and can take all of it with them.

### Nothing leaves the instance unless a guardian says so

No feature sends a child's drawing to a third party (an AI service, an
analytics tool) without the explicit, per-artist consent of a guardian. Off
by default.

## Cross-cutting constraints

- Images and data of minors: privacy by default, nothing public, nothing
  shared without an explicit action.
- Must run on modest hardware: a small VPS, a NAS or a Raspberry Pi.
- Must work from a phone: most drawings are captured with a phone camera.
- Bilingual from the start: Spanish and English.

## Out of scope by default

- A social network, public galleries or discovery between families.
- A print shop or any paid feature.
- A central hosted service holding many families' data.
- Native mobile apps.
- Federation between instances (export and import cover moving).

## Success criteria

- The founding family uses its instance for a month, with relatives other
  than the host uploading drawings, without the host having to chase them.
- A second family installs and runs an instance without the author's help.

## High-impact pending decisions

None blocks the first vertical slice. They live in `docs/OPEN_QUESTIONS.md` with
who answers them.
