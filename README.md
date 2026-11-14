# Pinacoteca

A private, self-hosted art gallery for the drawings children give to their
family.

Each drawing is photographed and kept with its story — who drew it, at what
age, what it shows, who received it — and exhibited only to the relatives the
child's family chooses. The archive is stored as plain files that stay
readable without Pinacoteca.

**Status:** Passkey accounts, galleries with roles and dedicated museum rooms, artist guardians (with persistent artwork visibility and ZIP export), JPEG EXIF orientation, crop and straighten upload editor, PWA offline support with queued uploads, and standalone HTML export. The gallery is closed until someone is invited. Works are shown only to members of a gallery that includes the artist.

## Run locally

```bash
go run ./cmd/pinacoteca invite --data ./data --name "Ada"
go run ./cmd/pinacoteca serve --data ./data
```

Open the printed link (it expires after 7 days), register a passkey, then
use http://localhost:8080. Create an artist. The artist slug is the directory
under `data/artists/`; the member id is the directory under `data/members/`.
Put them in a gallery or the home stays empty:

```bash
go run ./cmd/pinacoteca gallery create --data ./data --name "Grandparents"
go run ./cmd/pinacoteca gallery add-artist --data ./data --gallery grandparents --artist ARTIST_SLUG
go run ./cmd/pinacoteca gallery add-member --data ./data --gallery grandparents --member MEMBER_ID --role owner
```

Then upload a drawing and try:

```bash
rm ./data/index.db
go run ./cmd/pinacoteca reindex --data ./data
go run ./cmd/pinacoteca export --data ./data --artist ARTIST_SLUG --out ./export-artist
```

CLI commands and flags:
- `go run ./cmd/pinacoteca serve --data ./data [--addr :8080] [--base-url URL] [--rp-id HOST] [--origin URL]`
- `go run ./cmd/pinacoteca invite --data ./data --name NAME [--base-url URL]`
- `go run ./cmd/pinacoteca gallery create --data ./data --name NAME [--wall-colour COLOUR]`
- `go run ./cmd/pinacoteca gallery add-artist --data ./data --gallery SLUG --artist SLUG`
- `go run ./cmd/pinacoteca gallery add-member --data ./data --gallery SLUG --member ID --role ROLE`
- `go run ./cmd/pinacoteca artist add-guardian --data ./data ARTIST_SLUG MEMBER_ID`
- `go run ./cmd/pinacoteca export --data ./data --artist SLUG --out DIR [--lang es|en]`
- `go run ./cmd/pinacoteca reindex --data ./data`

## Check

```bash
make verify
```

Targets:
- `make verify` — run the full test suite and checks (fmt, vet, test, check-runtime-alpine, docker-build).
- `make fmt` — check that all Go files are formatted (`gofmt`).
- `make vet` — run Go static analyzer (`go vet ./...`).
- `make test` — run unit and integration tests (`go test ./...`).
- `make check-runtime-alpine` — verify the Alpine runtime version in Dockerfile is actively supported.
- `make docker-build` — build local container image (`pinacoteca:local`).

## Docker

```bash
docker build -t pinacoteca .
docker run --rm -p 8080:8080 -v pinacoteca-data:/data pinacoteca
```

- What it is and for whom: [`CONTEXT.md`](CONTEXT.md)
- Why it is built this way: [`docs/DECISIONS.md`](docs/DECISIONS.md)
- Architecture and codebase layout: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- Where it is going: [`docs/ROADMAP.md`](docs/ROADMAP.md)
- What is still open: [`docs/OPEN_QUESTIONS.md`](docs/OPEN_QUESTIONS.md)

Licensed under the [GNU AGPL-3.0](LICENSE).

### Artist guardians

Web artist creation makes the signed-in creator a provisional guardian and
approves the selected gallery. Other gallery requests remain pending until a
guardian approves them on the artist's galleries page, linked from the home
page. Gallery ownership alone cannot approve a request. Artists without
guardians retain their existing visibility.

The host can assign a guardian to an older artist (the member must already exist):

```sh
pinacoteca artist add-guardian --data ./data ARTIST_SLUG MEMBER_ID
```

Assignment does not approve existing galleries: the guardian must approve them.
Guardian IDs and approved gallery slugs live in `artist.json`; reindexing
preserves these permissions. CLI-created artists have no provisional guardian.
Guardians can also hide or show individual artworks (persisted in `artwork.json`)
and download an artist's archive as a ZIP from the web UI (`/artists/{artist}/export`).
Each gallery has a dedicated museum room view (`/galleries/{gallery}/room`).
