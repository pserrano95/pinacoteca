# Pinacoteca — Architecture and Codebase Map

This document describes the architectural structure and maps all packages, source files, and assets in the Pinacoteca codebase.

## 1. Architectural Foundations

Pinacoteca is built around several core architectural principles established in [`docs/DECISIONS.md`](DECISIONS.md) and [`CONTEXT.md`](../CONTEXT.md):

- **Filesystem Archive as Source of Truth (D-004):** Originals are immutable. Each artwork is stored in its own folder with an `artwork.json` metadata file and a `derived/` directory for generated artifacts (thumbnails, exports).
- **Rebuildable Index (D-004):** A SQLite database acts purely as a fast query index that can be rebuilt from scratch from the filesystem archive at any time (`reindex`).
- **Single Binary & Self-Contained Deployment (D-005):** Written in pure Go with modernc SQLite (no cgo) and packaged into a minimal Alpine container (`Dockerfile`).
- **Server-Rendered HTML with JavaScript Islands (D-006):** Go templates with htmx for dynamic interactions; vanilla JavaScript islands for authentication and offline service workers. No front-end build step.
- **Passkeys & Invite-Only Membership (D-009):** Authentication uses WebAuthn passkeys (fingerprint/face recognition) stored on disk as JSON (`members/`). New members join via cryptographic single-use invite tokens.
- **Museum Room Visual Theme (D-011):** Quiet museum wall exhibition with custom per-gallery wall colors, white mats, thin frame lines, and soft shadows.
- **Artist Guardians (D-008):** Parents retain full control over artists, approving gallery inclusions and controlling visibility.
- **Progressive Web App (PWA):** Standalone web app manifest, offline service worker, and verified icons for mobile and desktop installation.

---

## 2. Directory and File Map

### CLI (`cmd/pinacoteca/`)

- [`cmd/pinacoteca/main.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/cmd/pinacoteca/main.go): Main CLI entrypoint. Parses commands and flags:
  - `serve`: Start HTTP gallery server (`--data`, `--addr`, `--base-url`, `--rp-id`, `--origin`).
  - `invite`: Create a single-use 7-day invite link (`--data`, `--name`, `--base-url`).
  - `gallery create`: Create a gallery (`--data`, `--name`, `--wall-colour`).
  - `gallery add-artist`: Add artist to a gallery (`--data`, `--gallery`, `--artist`).
  - `gallery add-member`: Add member with role (`--data`, `--gallery`, `--member`, `--role`).
  - `artist add-guardian`: Assign member as guardian for an artist (`--data`, `ARTIST`, `MEMBER`).
  - `export`: Export artist works to static HTML (`--data`, `--artist`, `--out`, `--lang`).
  - `reindex`: Rebuild SQLite index from archive files on disk (`--data`).
- [`cmd/pinacoteca/artist_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/cmd/pinacoteca/artist_test.go): Tests for CLI artist management and guardian assignments.
- [`cmd/pinacoteca/export_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/cmd/pinacoteca/export_test.go): Tests for CLI artist export command.
- [`cmd/pinacoteca/gallery_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/cmd/pinacoteca/gallery_test.go): Tests for CLI gallery creation, artist associations, and member assignments.
- [`cmd/pinacoteca/invite_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/cmd/pinacoteca/invite_test.go): Tests for CLI invite generation and output format.
- [`cmd/pinacoteca/readme_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/cmd/pinacoteca/readme_test.go): Detector test ensuring all Makefile targets and codebase files remain documented in README and docs.

### Age Computation (`internal/age/`)

- [`internal/age/age.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/age/age.go): Computes child age (years and months) at artwork creation date based on artist birth date.
- [`internal/age/age_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/age/age_test.go): Unit tests for age calculation and localized string formatting.

### Standalone Export (`internal/export/`)

- [`internal/export/export.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/export/export.go): Exports all artworks of an artist to a standalone directory with static `index.html` and assets. Enforces canonical path resolution, symlink leak prevention, and transactional rollback on errors.
- [`internal/export/export_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/export/export_test.go): Tests verifying isolated output, SHA-256 integrity, symlink safety, and clean rollback.

### Internationalization (`internal/i18n/`)

- [`internal/i18n/i18n.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/i18n/i18n.go): String translation loader and localized formatter supporting Spanish (`es`) and English (`en`).
- [`internal/i18n/locales/en.json`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/i18n/locales/en.json): English localization catalog.
- [`internal/i18n/locales/es.json`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/i18n/locales/es.json): Spanish localization catalog.

### Image Processing (`internal/imageutil/`)

- [`internal/imageutil/imageutil.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/imageutil/imageutil.go): Image format inspection, EXIF orientation correction for JPEG images (supporting all 8 TIFF/EXIF orientations in little-endian II and big-endian MM), and thumbnail generation (`thumb.jpg`).
- [`internal/imageutil/imageutil_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/imageutil/imageutil_test.go): Unit tests verifying lossless orientation transforms and immutable original preservation.

### Query Index (`internal/index/`)

- [`internal/index/index.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/index/index.go): SQLite database indexing artists, artworks, galleries, members, permissions, and web sessions. Supports full rebuilds from the filesystem.
- [`internal/index/reindex_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/index/reindex_test.go): Verifies that index rebuilds faithfully restore artist, artwork, gallery, and guardian states.

### Slug Generation (`internal/slug/`)

- [`internal/slug/slug.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/slug/slug.go): Sanitizes names into filesystem-safe, URL-safe slugs for directories and routes.

### Filesystem Storage (`internal/store/`)

- [`internal/store/store.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/store.go): Archive filesystem layout manager, artwork directory writer, thumbnail storage, and atomic file writes.
- [`internal/store/store_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/store_test.go): Tests for artwork creation, folder layout, and filesystem persistence.
- [`internal/store/galleries.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/galleries.go): Gallery creation, membership management, and concurrency locking for `gallery.json`.
- [`internal/store/galleries_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/galleries_test.go): Tests for gallery CRUD and member role assignments.
- [`internal/store/galleries_concurrency_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/galleries_concurrency_test.go): Concurrency stress tests asserting zero lost writes during concurrent gallery modifications.
- [`internal/store/guardians.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/guardians.go): Guardian assignment and per-artist approved gallery tracking persisted in `artist.json`.
- [`internal/store/guardians_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/guardians_test.go): Tests verifying guardian permissions persist across reindexes.
- [`internal/store/invites.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/invites.go): Single-use cryptographic invite creation and redemption persisted on disk.
- [`internal/store/members.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/members.go): Passkey credential and member profile disk persistence (`member.json`, `credentials.json`).
- [`internal/store/types.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/store/types.go): Core domain types (`Artwork`, `Artist`, `Gallery`, `Member`, `Role`, `Invite`).

### Web Interface & Authentication (`internal/web/`)

- [`internal/web/server.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/server.go): HTTP router, handlers, template execution, and session management.
- [`internal/web/auth.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/auth.go): WebAuthn passkey ceremony endpoints (`/login`, `/invite/{token}`) and session cookie handling.
- [`internal/web/auth_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/auth_test.go): Tests for passkey registration and login flows.
- [`internal/web/softauth_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/softauth_test.go): Tests ensuring unauthenticated requests are refused.
- [`internal/web/gallery_access_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/gallery_access_test.go): Enforces viewer, contributor, and owner permissions on artworks and uploads.
- [`internal/web/gallery_web_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/gallery_web_test.go): Tests web interface flows for gallery management and browsing.
- [`internal/web/guardians_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/guardians_test.go): Tests guardian approval workflows in web interface.
- [`internal/web/htmx_static_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/htmx_static_test.go): Validates embedded htmx script behavior.
- [`internal/web/pwa_test.go`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/pwa_test.go): Tests PWA manifest, service worker registration, and icon serving.
- [`internal/web/templates/pages.html`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/templates/pages.html): Go HTML templates for gallery pages, artwork views, upload forms, and invites.
- [`internal/web/testdata/htmx.min.test.js`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/testdata/htmx.min.test.js): Vendored test fixture for htmx behavior suite.
- [`internal/web/static/auth.js`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/auth.js): Client-side WebAuthn ceremony script.
- [`internal/web/static/htmx.min.js`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/htmx.min.js): Embedded htmx library (v2.0.4) for dynamic HTML islands.
- [`internal/web/static/pwa.js`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/pwa.js): Client script registering the service worker.
- [`internal/web/static/style.css`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/style.css): Stylesheet implementing the quiet museum room exhibition theme (D-011).
- [`internal/web/static/sw.js`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/sw.js): PWA service worker with offline caching.
- [`internal/web/static/manifest.webmanifest`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/manifest.webmanifest): Web application manifest for PWA installation.
- [`internal/web/static/icons/apple-touch-icon.png`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/icons/apple-touch-icon.png): Apple touch icon.
- [`internal/web/static/icons/icon-180.png`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/icons/icon-180.png): 180x180 app icon.
- [`internal/web/static/icons/icon-192.png`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/icons/icon-192.png): 192x192 PWA standard icon.
- [`internal/web/static/icons/icon-512.png`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/icons/icon-512.png): 512x512 high-resolution icon.
- [`internal/web/static/icons/icon-512-maskable.png`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/internal/web/static/icons/icon-512-maskable.png): 512x512 maskable icon for adaptive display.

### Scripts & Build (`scripts/`, Root)

- [`scripts/check-runtime-alpine.sh`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/scripts/check-runtime-alpine.sh): Verifies runtime Alpine base image in `Dockerfile` is supported and not EOL.
- [`Makefile`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/Makefile): Build and test automation targets (`verify`, `fmt`, `vet`, `test`, `check-runtime-alpine`, `docker-build`).
- [`Dockerfile`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/Dockerfile): Multi-stage container definition (Go build + Alpine runtime).
- [`.dockerignore`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/.dockerignore): Docker build context exclusion rules.
- [`.gitignore`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/.gitignore): Repository git ignore rules.
- [`go.mod`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/go.mod): Go module declaration and dependencies.
- [`go.sum`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/go.sum): Checksums of direct and transitive Go dependencies.
- [`.github/workflows/ci.yml`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/.github/workflows/ci.yml): Main CI pipeline executing `make verify`.
- [`.github/workflows/arranque.yml`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/.github/workflows/arranque.yml): Cold-start verification workflow testing from fresh Go install.
- [`.github/dependabot.yml`](file:///Users/pabloserrano/orca/workspaces/pinacoteca/auto-pinacoteca-36/.github/dependabot.yml): Automated dependency updates configuration.
