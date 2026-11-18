package e2e

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
	"github.com/pserrano95/pinacoteca/internal/web"
)

// Harness runs an isolated local Pinacoteca server against a temporary
// filesystem archive seeded with sample artworks and a pre-authenticated
// session (no external services, no hardware passkey).
type Harness struct {
	BaseURL             string
	SessionCookie       string
	ViewerSessionCookie string
	GallerySlug         string
	ArtistSlug          string
	Artwork1Folder      string
	Artwork2Folder      string
	SampleImagePath     string

	dir    string
	ix     *index.Index
	server *http.Server
	ln     net.Listener
}

// StartHarness creates a fresh temporary data directory, seeds a family
// gallery with sample drawings, and starts the HTTP server on 127.0.0.1:0.
func StartHarness() (*Harness, error) {
	dir, err := os.MkdirTemp("", "pinacoteca-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	cleanupDir := true
	defer func() {
		if cleanupDir {
			_ = os.RemoveAll(dir)
		}
	}()

	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		return nil, fmt.Errorf("ensure layout: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	ana, err := st.CreateMember("Ana", []store.Passkey{{ID: "e2e-cred-ana", PublicKey: "e2e-pubkey-ana"}}, now)
	if err != nil {
		return nil, fmt.Errorf("create member: %w", err)
	}
	vera, err := st.CreateMember("Vera", []store.Passkey{{ID: "e2e-cred-vera", PublicKey: "e2e-pubkey-vera"}}, now)
	if err != nil {
		return nil, fmt.Errorf("create viewer member: %w", err)
	}

	gal, err := st.CreateGallery("Family Room", "pale-sage")
	if err != nil {
		return nil, fmt.Errorf("create gallery: %w", err)
	}
	if err := st.AddGalleryMember(gal.Slug, ana.ID, store.RoleOwner); err != nil {
		return nil, fmt.Errorf("add gallery member: %w", err)
	}
	if err := st.AddGalleryMember(gal.Slug, vera.ID, store.RoleViewer); err != nil {
		return nil, fmt.Errorf("add gallery viewer: %w", err)
	}

	birth := time.Date(2019, 6, 15, 0, 0, 0, 0, time.UTC)
	artist, err := st.CreateArtistWithGuardian("Sofia", birth, ana.ID)
	if err != nil {
		return nil, fmt.Errorf("create artist: %w", err)
	}
	if err := st.AddGalleryArtist(gal.Slug, artist.Slug); err != nil {
		return nil, fmt.Errorf("add gallery artist: %w", err)
	}
	if err := st.ApproveArtistGallery(artist.Slug, gal.Slug, ana.ID); err != nil {
		return nil, fmt.Errorf("approve artist gallery: %w", err)
	}

	sunImg := samplePNG(240, 180, color.RGBA{R: 245, G: 210, B: 80, A: 255}, color.RGBA{R: 205, G: 75, B: 60, A: 255})
	dragonImg := samplePNG(240, 180, color.RGBA{R: 70, G: 130, B: 200, A: 255}, color.RGBA{R: 40, G: 170, B: 120, A: 255})

	art1, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  artist.Slug,
		Title:       "Sun and House",
		Date:        time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC),
		Description: "A bright yellow sun over a red house",
		Tags:        []string{"sun", "house"},
		GiftedTo:    "Abuela",
		ImageBytes:  sunImg,
	})
	if err != nil {
		return nil, fmt.Errorf("create artwork 1: %w", err)
	}
	art2, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  artist.Slug,
		Title:       "Blue Dragon",
		Date:        time.Date(2025, 4, 2, 0, 0, 0, 0, time.UTC),
		Description: "A friendly blue dragon",
		Tags:        []string{"dragon"},
		GiftedTo:    "Tio Luis",
		ImageBytes:  dragonImg,
	})
	if err != nil {
		return nil, fmt.Errorf("create artwork 2: %w", err)
	}

	samplePath := filepath.Join(dir, "sample-upload.png")
	uploadImg := samplePNG(240, 180, color.RGBA{R: 160, G: 100, B: 210, A: 255}, color.RGBA{R: 250, G: 180, B: 70, A: 255})
	if err := os.WriteFile(samplePath, uploadImg, 0o600); err != nil {
		return nil, fmt.Errorf("write sample image: %w", err)
	}

	ix, err := index.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	cleanupIndex := true
	defer func() {
		if cleanupIndex {
			_ = ix.Close()
		}
	}()

	if err := ix.Rebuild(st); err != nil {
		return nil, fmt.Errorf("rebuild index: %w", err)
	}

	const sessionTok = "e2e-session-token"
	if err := ix.CreateSession(sessionTok, ana.ID, now, now.Add(24*time.Hour)); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	const viewerTok = "e2e-viewer-session-token"
	if err := ix.CreateSession(viewerTok, vera.ID, now, now.Add(24*time.Hour)); err != nil {
		return nil, fmt.Errorf("create viewer session: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	cfg, err := web.ConfigFromBase(baseURL, "localhost", "")
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("config: %w", err)
	}
	srv, err := web.New(st, ix, cfg)
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("web server: %w", err)
	}

	httpSrv := &http.Server{Handler: srv.Handler()}
	go func() {
		_ = httpSrv.Serve(ln)
	}()

	cleanupDir = false
	cleanupIndex = false
	return &Harness{
		BaseURL:             baseURL,
		SessionCookie:       sessionTok,
		ViewerSessionCookie: viewerTok,
		GallerySlug:         gal.Slug,
		ArtistSlug:          artist.Slug,
		Artwork1Folder:      art1.Folder,
		Artwork2Folder:      art2.Folder,
		SampleImagePath:     samplePath,
		dir:                 dir,
		ix:                  ix,
		server:              httpSrv,
		ln:                  ln,
	}, nil
}

// Close stops the HTTP server, closes the index, and removes the temp directory.
func (h *Harness) Close() {
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.server.Shutdown(ctx)
	}
	if h.ix != nil {
		_ = h.ix.Close()
	}
	if h.dir != "" {
		_ = os.RemoveAll(h.dir)
	}
}

func samplePNG(w, h int, bg, fg color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x > w/4 && x < 3*w/4 && y > h/4 && y < 3*h/4 {
				img.Set(x, y, fg)
			} else {
				img.Set(x, y, bg)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
