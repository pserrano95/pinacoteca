---
name: e2e
description: >
  Verify Pinacoteca as a person uses it: starts a local seeded instance, drives
  it in a real Chromium browser, and reports which step broke with a screenshot.
  Run ALWAYS before opening a PR or finishing any change to `internal/web/`,
  `internal/i18n/`, or `internal/e2e/`; when adding or changing anything a
  person sees or clicks (its journey goes in the same PR); and whenever asked
  if part of the gallery works. Does not replace `make verify`: runs after it.
---

# e2e

`make verify` checks that the Go code compiles, passes `vet`, and passes unit
and HTTP tests. It does not check that the gallery **can be used**: a button
covered by another layer, a screen overflowing a 390 px phone viewport, or a
JavaScript error on load passes all of those tests. `make e2e` opens the
gallery in a real browser and walks through it.

## How to run it

```bash
make e2e                        # all journeys
make e2e ARGS="view-artwork"    # one or more journeys, by id
make e2e ARGS="--list"          # list all journeys and steps without running
```

Requires a Chromium or Chrome binary (`google-chrome`, `chromium`,
`chromium-browser` in `PATH`, macOS Chrome, or `E2E_CHROMIUM=/path/to/bin`).

Nothing leaves the machine: each journey starts its own isolated temporary
archive and local HTTP server (`internal/e2e/harness.go`), seeded with sample
drawings and a session cookie in SQLite.

## How to read the output

- Exits with **0** and `N green, M known` if no unexpected failures occurred.
  A known assertion failure is marked `⚠` and its journey continues.
  A known assertion that passes fails the run and must be removed from
  `CONOCIDOS`.
- Exits with **1**: prints the journey, the **step** that failed and why, and
  saves a full-page screenshot in `e2e/output/<id>.png`. **Open the
  screenshot** before touching anything: it almost always shows what happened.
- Besides its steps, every journey requires: no uncaught JavaScript errors,
  no `console.error` from the app, no same-origin HTTP errors, no unexpected
  resource load failures and no external requests. Declare deliberate HTTP
  4xx responses by path and status in the step that expects them. Browser
  noise still fails the run in a step listed in `CONOCIDOS`.
- Failing to start the browser or the harness is **red**: not having looked
  is not passing. Never report it as green in the PR.

## What to do with a red

1. Read the failing step and open the screenshot in `e2e/output/<id>.png`.
2. Fix **the application** if your change caused the failure. Re-run that
   journey, and at the end run all of them.
3. If it is still red on the third attempt, stop and report it in the PR with
   the pasted output: do not keep guessing.

If a journey exposes a real failure unrelated to the assignment, report it in
the PR under **Repository surprises** with the screenshot and a proposed
`CONOCIDOS` entry naming the journey, exact step, reason and repository issue.
Leave the application and the list untouched. Only a session with the
repository owner or PR review may add an entry.

What you **never** do to turn it green: raise `Patience`, add fixed sleeps,
retry until it passes, drop a step, or remove a journey. A flaky journey has
a root cause: find it. If you cannot find it, say so in the PR; do not hide
it.

## The map: `internal/e2e/journeys.go`

Each journey is something a person can do, seen from their side: where it
starts, what they click or submit, and **what they end up seeing**. The last
step checks the visible outcome, not just that a button exists.

- **New feature in the gallery** -> add its journey in the same PR. Prefer
  selectors tied to user-visible text, form field names, or explicit action
  routes over fragile structural selectors.
- **Changed feature** -> update its journey in the same PR.
- **New screen** -> add it to `screens` in `internal/e2e/journeys.go` so it is
  checked by title on both desktop and 390 px mobile without horizontal
  overflow.
- A new journey is tested by **breaking what it guards on purpose** (and
  reverting the breakage): if it stays green while the feature is broken, it
  guards nothing.

Changing or removing an existing journey, or changing the runner
(`internal/e2e/runner.go`, `internal/e2e/journey.go`), changes how work is
judged: explain it in the PR and it is reviewed by the repository owner.

## What this does not see

- Uses a pre-seeded session cookie in SQLite instead of a real WebAuthn
  hardware passkey ceremony (the `/login` page itself is opened unauthenticated).
- Does not test production deployments, TLS, or reverse-proxy configuration.
- Checks that screens fit and work, not that they look pretty.

## In the PR

Paste the output (`N green, M known`) alongside the output of `make
verify`. If it was not run, state: `e2e: not run, because ...`.
