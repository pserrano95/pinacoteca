package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/pserrano95/pinacoteca/internal/i18n"
	"github.com/pserrano95/pinacoteca/internal/imageutil"
	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

const maxUploadBytes = 20 << 20 // 20 MiB

// Server serves the gallery UI and mutates the filesystem archive.
type Server struct {
	Store *store.Store
	Index *index.Index
	tmpl  *template.Template

	cfg    Config
	wa     *webauthn.WebAuthn
	secure bool

	mu         sync.Mutex
	ceremonies map[string]ceremony
	redeemMu   sync.Mutex
}

func New(st *store.Store, ix *index.Index, cfg Config) (*Server, error) {
	if cfg.RPDisplayName == "" {
		cfg.RPDisplayName = i18n.T("en", "app_name", nil)
	}
	wa, err := newWebAuthn(cfg)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.ParseFS(assets, "templates/pages.html")
	if err != nil {
		return nil, err
	}
	return &Server{
		Store:      st,
		Index:      ix,
		tmpl:       tmpl,
		cfg:        cfg,
		wa:         wa,
		secure:     strings.HasPrefix(cfg.Origin, "https://"),
		ceremonies: map[string]ceremony{},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	handlers := map[string]http.Handler{
		"GET /static/":                            http.StripPrefix("/static/", http.FileServer(http.FS(static))),
		"GET /login":                              http.HandlerFunc(s.loginGET),
		"GET /invite/{token}":                     http.HandlerFunc(s.inviteGET),
		"POST /login/begin":                       http.HandlerFunc(s.loginBegin),
		"POST /login/finish":                      http.HandlerFunc(s.loginFinish),
		"POST /invite/{token}/begin":              http.HandlerFunc(s.inviteBegin),
		"POST /invite/{token}/finish":             http.HandlerFunc(s.inviteFinish),
		"GET /{$}":                                http.HandlerFunc(s.gallery),
		"GET /artists/new":                        http.HandlerFunc(s.artistNewGET),
		"POST /artists/new":                       http.HandlerFunc(s.artistNewPOST),
		"GET /artworks/new":                       http.HandlerFunc(s.artworkNewGET),
		"POST /artworks/new":                      http.HandlerFunc(s.artworkNewPOST),
		"GET /artists/{artist}/artworks/{folder}": http.HandlerFunc(s.artworkGET),
		"GET /artists/{artist}/artworks/{folder}/original": http.HandlerFunc(s.serveOriginal),
		"GET /artists/{artist}/artworks/{folder}/thumb":    http.HandlerFunc(s.serveThumb),
		"POST /logout": http.HandlerFunc(s.logout),
	}
	for _, rt := range s.routeSpecs() {
		key := rt.Method + " " + rt.Pattern
		h, ok := handlers[key]
		if !ok {
			panic("missing handler for " + key)
		}
		if rt.Method == http.MethodPost {
			h = s.withCSRF(h)
		}
		if !rt.Public {
			h = s.withSession(h, rt.Fail)
		}
		mux.Handle(key, h)
	}
	return mux
}

type pageData struct {
	Lang           string
	Title          string
	Flash          string
	Error          string
	T              func(string) string
	AgeLabel       func(int) string
	Artworks       []artworkView
	Artists        []store.Artist
	SelectedArtist string
	Artwork        *artworkView
	ByLine         string
	AgeLine        string
	CSRF           string
	LoggedIn       bool
	Lead           string
	AuthMode       string
	BeginURL       string
	FinishURL      string
}

type artworkView struct {
	ArtistSlug  string
	ArtistName  string
	Folder      string
	Title       string
	Date        string
	Description string
	Tags        string
	GiftedTo    string
	AgeYears    int
	HasThumb    bool
}

func (s *Server) base(lang string) pageData {
	lang = i18n.NormalizeLang(lang)
	return pageData{
		Lang: lang,
		T: func(key string) string {
			return i18n.T(lang, key, nil)
		},
		AgeLabel: func(age int) string {
			return i18n.T(lang, "gallery_age", map[string]string{"age": strconv.Itoa(age)})
		},
	}
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) pageData {
	data := s.base(langFrom(r))
	data.CSRF = s.ensureCSRF(w, r)
	if _, ok := s.sessionMember(r); ok {
		data.LoggedIn = true
	}
	return data
}

func (s *Server) render(w http.ResponseWriter, name string, data pageData) {
	s.renderStatus(w, http.StatusOK, name, data)
}

func (s *Server) renderStatus(w http.ResponseWriter, status int, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		// Status is already committed; nothing more useful to send.
		return
	}
}

func langFrom(r *http.Request) string {
	if err := r.ParseForm(); err == nil {
		if v := r.FormValue("lang"); v != "" {
			return v
		}
	}
	if v := r.URL.Query().Get("lang"); v != "" {
		return v
	}
	return "en"
}

func (s *Server) gallery(w http.ResponseWriter, r *http.Request) {
	data := s.page(w, r)
	data.Title = data.T("gallery_title")
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = data.T(flash)
	}
	rows, err := s.Index.ListArtworks()
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery", data)
		return
	}
	for _, row := range rows {
		thumb := filepath.Join(s.Store.ArtistsRoot(), row.ArtistSlug, row.Folder, store.DerivedDir, store.ThumbName)
		data.Artworks = append(data.Artworks, artworkView{
			ArtistSlug:  row.ArtistSlug,
			ArtistName:  row.ArtistName,
			Folder:      row.Folder,
			Title:       displayTitle(row.Title),
			Date:        row.Date.Format("2006-01-02"),
			Description: row.Description,
			Tags:        strings.Join(row.Tags, ", "),
			GiftedTo:    row.GiftedTo,
			AgeYears:    row.AgeYears,
			HasThumb:    fileExists(thumb),
		})
	}
	s.render(w, "gallery", data)
}

func (s *Server) artistNewGET(w http.ResponseWriter, r *http.Request) {
	data := s.page(w, r)
	data.Title = data.T("artist_new_title")
	s.render(w, "artist_new", data)
}

func (s *Server) artistNewPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	data := s.page(w, r)
	lang := data.Lang
	data.Title = data.T("artist_new_title")
	name := r.FormValue("name")
	birth, err := store.ParseDate(r.FormValue("birth_date"))
	if err != nil || strings.TrimSpace(name) == "" {
		data.Error = data.T("error_required")
		s.render(w, "artist_new", data)
		return
	}
	if _, err := s.Store.CreateArtist(name, birth.Time); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artist_new", data)
		return
	}
	if err := s.Index.RebuildKeepingSessions(s.Store); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artist_new", data)
		return
	}
	http.Redirect(w, r, "/?lang="+lang+"&flash=artist_created", http.StatusSeeOther)
}

func (s *Server) artworkNewGET(w http.ResponseWriter, r *http.Request) {
	data := s.page(w, r)
	data.Title = data.T("upload_title")
	artists, err := s.Store.ListArtists()
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artwork_new", data)
		return
	}
	data.Artists = artists
	data.SelectedArtist = r.URL.Query().Get("artist")
	s.render(w, "artwork_new", data)
}

func (s *Server) artworkNewPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		data := s.page(w, r)
		data.Title = data.T("upload_title")
		data.Error = data.T("error_generic")
		s.render(w, "artwork_new", data)
		return
	}
	data := s.page(w, r)
	lang := data.Lang
	data.Title = data.T("upload_title")
	artists, _ := s.Store.ListArtists()
	data.Artists = artists
	artistSlug := r.FormValue("artist_slug")
	data.SelectedArtist = artistSlug
	date, err := store.ParseDate(r.FormValue("date"))
	if err != nil || artistSlug == "" {
		data.Error = data.T("error_required")
		s.render(w, "artwork_new", data)
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		data.Error = data.T("error_required")
		s.render(w, "artwork_new", data)
		return
	}
	defer file.Close()
	bytes, err := imageutil.ReadLimited(file, maxUploadBytes)
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artwork_new", data)
		return
	}
	_, err = s.Store.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  artistSlug,
		Title:       r.FormValue("title"),
		Date:        date.Time,
		Description: r.FormValue("description"),
		Tags:        splitTags(r.FormValue("tags")),
		GiftedTo:    r.FormValue("gifted_to"),
		ImageBytes:  bytes,
	})
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "not an allowed image") || strings.Contains(msg, "not a valid image"):
			data.Error = data.T("error_not_image")
		case strings.Contains(msg, "before birth"):
			data.Error = data.T("error_before_birth")
		default:
			data.Error = data.T("error_generic")
		}
		s.render(w, "artwork_new", data)
		return
	}
	if err := s.Index.RebuildKeepingSessions(s.Store); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artwork_new", data)
		return
	}
	http.Redirect(w, r, "/?lang="+lang+"&flash=upload_success", http.StatusSeeOther)
}

func (s *Server) artworkGET(w http.ResponseWriter, r *http.Request) {
	data := s.page(w, r)
	lang := data.Lang
	artist := r.PathValue("artist")
	folder := r.PathValue("folder")
	row, err := s.Index.GetArtwork(artist, folder)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	thumb := filepath.Join(s.Store.ArtistsRoot(), row.ArtistSlug, row.Folder, store.DerivedDir, store.ThumbName)
	view := &artworkView{
		ArtistSlug:  row.ArtistSlug,
		ArtistName:  row.ArtistName,
		Folder:      row.Folder,
		Title:       displayTitle(row.Title),
		Date:        row.Date.Format("2006-01-02"),
		Description: row.Description,
		Tags:        strings.Join(row.Tags, ", "),
		GiftedTo:    row.GiftedTo,
		AgeYears:    row.AgeYears,
		HasThumb:    fileExists(thumb),
	}
	data.Artwork = view
	data.Title = view.Title
	data.ByLine = i18n.T(lang, "artwork_by", map[string]string{"name": row.ArtistName})
	data.AgeLine = i18n.T(lang, "artwork_age", map[string]string{"age": strconv.Itoa(row.AgeYears)})
	s.render(w, "artwork", data)
}

func (s *Server) serveOriginal(w http.ResponseWriter, r *http.Request) {
	rec, err := s.Store.LoadArtwork(r.PathValue("artist"), r.PathValue("folder"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, rec.OriginalPath)
}

func (s *Server) serveThumb(w http.ResponseWriter, r *http.Request) {
	rec, err := s.Store.LoadArtwork(r.PathValue("artist"), r.PathValue("folder"))
	if err != nil || rec.ThumbPath == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, rec.ThumbPath)
}

func splitTags(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func displayTitle(t string) string {
	if strings.TrimSpace(t) == "" {
		return "—"
	}
	return t
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
