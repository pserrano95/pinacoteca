package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
	"github.com/pserrano95/pinacoteca/internal/web"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	switch cmd {
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "reindex":
		if err := runReindex(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `pinacoteca — private family art gallery

Usage:
  pinacoteca serve   --data DIR [--addr HOST:PORT]
  pinacoteca reindex --data DIR

`)
}

func dataFlag(fs *flag.FlagSet) *string {
	return fs.String("data", "./data", "directory for the archive and index")
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := dataFlag(fs)
	addr := fs.String("addr", ":8080", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dataDir, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	st := store.New(dataDir)
	if err := st.EnsureLayout(); err != nil {
		return err
	}
	ix, err := index.Open(dataDir)
	if err != nil {
		return err
	}
	defer ix.Close()
	if err := ix.Rebuild(st); err != nil {
		return fmt.Errorf("initial index: %w", err)
	}
	srv, err := web.New(st, ix)
	if err != nil {
		return err
	}
	log.Printf("pinacoteca serving on %s (data %s)", *addr, dataDir)
	return http.ListenAndServe(*addr, srv.Handler())
}

func runReindex(args []string) error {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	data := dataFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dataDir, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	st := store.New(dataDir)
	if err := st.EnsureLayout(); err != nil {
		return err
	}
	ix, err := index.Open(dataDir)
	if err != nil {
		return err
	}
	defer ix.Close()
	if err := ix.Rebuild(st); err != nil {
		return err
	}
	snap, err := ix.Snapshot()
	if err != nil {
		return err
	}
	log.Printf("reindexed %d artists, %d artworks from %s", len(snap.Artists), len(snap.Artworks), dataDir)
	return nil
}
