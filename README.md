# Pinacoteca

A private, self-hosted art gallery for the drawings children give to their
family.

Each drawing is photographed and kept with its story — who drew it, at what
age, what it shows, who received it — and exhibited only to the relatives the
child's family chooses. The archive is stored as plain files that stay
readable without Pinacoteca.

**Status:** H0 skeleton — a single Go binary that stores drawings on disk and
rebuilds its SQLite index from them.

## Run locally

```bash
go run ./cmd/pinacoteca serve --data ./data
```

Open http://localhost:8080. Create an artist, upload a drawing, then try:

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
