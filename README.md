# Pinacoteca

A private, self-hosted art gallery for the drawings children give to their
family.

Each drawing is photographed and kept with its story — who drew it, at what
age, what it shows, who received it — and exhibited only to the relatives the
child's family chooses. The archive is stored as plain files that stay
readable without Pinacoteca.

**Status:** H0 skeleton, passkey accounts, and galleries with roles. The
gallery is closed until someone is invited. Works are shown only to members
of a gallery that includes the artist.

## Run locally

```bash
go run ./cmd/pinacoteca invite --data ./data --name "Ada"
go run ./cmd/pinacoteca serve --data ./data
```

Open the printed link (it expires after 7 days), register a passkey, then
use http://localhost:8080. Create an artist and upload a drawing. The artist
slug is the directory under `data/artists/`; the member id is the directory
under `data/members/`. Put them in a gallery or the home stays empty:

```bash
go run ./cmd/pinacoteca gallery create --data ./data --name "Grandparents"
go run ./cmd/pinacoteca gallery add-artist --data ./data --gallery grandparents --artist ARTIST_SLUG
go run ./cmd/pinacoteca gallery add-member --data ./data --gallery grandparents --member MEMBER_ID --role owner
```

Then try:

```bash
rm ./data/index.db
go run ./cmd/pinacoteca reindex --data ./data
```

## Check

```bash
make verify
```

## Docker

```bash
docker build -t pinacoteca .
docker run --rm -p 8080:8080 -v pinacoteca-data:/data pinacoteca
```

- What it is and for whom: [`CONTEXT.md`](CONTEXT.md)
- Why it is built this way: [`docs/DECISIONS.md`](docs/DECISIONS.md)
- Where it is going: [`docs/ROADMAP.md`](docs/ROADMAP.md)
- What is still open: [`docs/OPEN_QUESTIONS.md`](docs/OPEN_QUESTIONS.md)

Licensed under the [GNU AGPL-3.0](LICENSE).
