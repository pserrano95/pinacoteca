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
}

func New(st *store.Store, ix *index.Index) (*Server, error) {
	tmpl, err := template.ParseFS(assets, "templates/pages.html")
	if err != nil {
		return nil, err
	}
	return &Server{Store: st, Index: ix, tmpl: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /{$}", s.gallery)
	mux.HandleFunc("GET /artists/new", s.artistNewGET)
	mux.HandleFunc("POST /artists/new", s.artistNewPOST)
	mux.HandleFunc("GET /artworks/new", s.artworkNewGET)
	mux.HandleFunc("POST /artworks/new", s.artworkNewPOST)
	mux.HandleFunc("GET /artists/{artist}/artworks/{folder}", s.artworkGET)
	mux.HandleFunc("GET /artists/{artist}/artworks/{folder}/original", s.serveOriginal)
	mux.HandleFunc("GET /artists/{artist}/artworks/{folder}/thumb", s.serveThumb)
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

func (s *Server) render(w http.ResponseWriter, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	data := s.base(langFrom(r))
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
	data := s.base(langFrom(r))
	data.Title = data.T("artist_new_title")
	s.render(w, "artist_new", data)
}

func (s *Server) artistNewPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	lang := langFrom(r)
	data := s.base(lang)
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
	if err := s.Index.Rebuild(s.Store); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artist_new", data)
		return
	}
	http.Redirect(w, r, "/?lang="+lang+"&flash=artist_created", http.StatusSeeOther)
}

func (s *Server) artworkNewGET(w http.ResponseWriter, r *http.Request) {
	data := s.base(langFrom(r))
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
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "en"
	}
	data := s.base(lang)
	data.Title = data.T("upload_title")
	artists, _ := s.Store.ListArtists()
	data.Artists = artists

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artwork_new", data)
		return
	}
	if v := r.FormValue("lang"); v != "" {
		lang = v
		data = s.base(lang)
		data.Title = data.T("upload_title")
		data.Artists = artists
	}
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
	if err := s.Index.Rebuild(s.Store); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artwork_new", data)
		return
	}
	http.Redirect(w, r, "/?lang="+lang+"&flash=upload_success", http.StatusSeeOther)
}

func (s *Server) artworkGET(w http.ResponseWriter, r *http.Request) {
	lang := langFrom(r)
	data := s.base(lang)
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
