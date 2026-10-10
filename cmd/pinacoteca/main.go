package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	case "invite":
		if err := runInvite(os.Args[2:]); err != nil {
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
  pinacoteca serve   --data DIR [--addr HOST:PORT] [--base-url URL] [--rp-id HOST] [--origin URL]
  pinacoteca invite  --data DIR --name NAME [--base-url URL]
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
	baseURL := fs.String("base-url", "http://localhost:8080", "public URL of this instance (WebAuthn origin and invite links)")
	rpID := fs.String("rp-id", "", "WebAuthn relying party id (default: host of --base-url)")
	origin := fs.String("origin", "", "WebAuthn origin (default: --base-url)")
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
	if err := ix.RebuildKeepingSessions(st); err != nil {
		return fmt.Errorf("initial index: %w", err)
	}
	cfg, err := web.ConfigFromBase(*baseURL, *rpID, *origin)
	if err != nil {
		return err
	}
	srv, err := web.New(st, ix, cfg)
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
	log.Printf("reindexed %d artists, %d artworks, %d members from %s", len(snap.Artists), len(snap.Artworks), len(snap.Members), dataDir)
	return nil
}

func runInvite(args []string) error {
	fs := flag.NewFlagSet("invite", flag.ContinueOnError)
	data := dataFlag(fs)
	name := fs.String("name", "", "display name of the person being invited")
	baseURL := fs.String("base-url", "http://localhost:8080", "public URL used in the invite link")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("invite requires --name")
	}
	dataDir, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	st := store.New(dataDir)
	if err := st.EnsureLayout(); err != nil {
		return err
	}
	link, err := st.CreateInvite(*name, *baseURL, time.Now())
	if err != nil {
		return err
	}
	fmt.Println(link)
	return nil
}
