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
	"time"

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
		"GET /static/":                                     http.StripPrefix("/static/", http.FileServer(http.FS(static))),
		"GET /manifest.webmanifest":                        http.HandlerFunc(s.manifestGET),
		"GET /sw.js":                                       http.HandlerFunc(s.swGET),
		"GET /login":                                       http.HandlerFunc(s.loginGET),
		"GET /invite/{token}":                              http.HandlerFunc(s.inviteGET),
		"POST /login/begin":                                http.HandlerFunc(s.loginBegin),
		"POST /login/finish":                               http.HandlerFunc(s.loginFinish),
		"POST /invite/{token}/begin":                       http.HandlerFunc(s.inviteBegin),
		"POST /invite/{token}/finish":                      http.HandlerFunc(s.inviteFinish),
		"GET /{$}":                                         http.HandlerFunc(s.gallery),
		"GET /galleries/new":                               http.HandlerFunc(s.galleryNewGET),
		"POST /galleries/new":                              http.HandlerFunc(s.galleryNewPOST),
		"GET /galleries/{gallery}":                         http.HandlerFunc(s.galleryManageGET),
		"POST /galleries/{gallery}/artists":                http.HandlerFunc(s.galleryArtistPOST),
		"POST /galleries/{gallery}/invite":                 http.HandlerFunc(s.galleryInvitePOST),
		"GET /artists/new":                                 http.HandlerFunc(s.artistNewGET),
		"POST /artists/new":                                http.HandlerFunc(s.artistNewPOST),
		"GET /artworks/new":                                http.HandlerFunc(s.artworkNewGET),
		"POST /artworks/new":                               http.HandlerFunc(s.artworkNewPOST),
		"GET /artists/{artist}/artworks/{folder}":          http.HandlerFunc(s.artworkGET),
		"GET /artists/{artist}/artworks/{folder}/original": http.HandlerFunc(s.serveOriginal),
		"GET /artists/{artist}/artworks/{folder}/thumb":    http.HandlerFunc(s.serveThumb),
		"POST /logout":                                     http.HandlerFunc(s.logout),
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
	Lang            string
	Title           string
	Flash           string
	Error           string
	T               func(string) string
	AgeLabel        func(int) string
	Artworks        []artworkView
	Artists         []store.Artist
	SelectedArtist  string
	SelectedGallery string
	Artwork         *artworkView
	ByLine          string
	AgeLine         string
	CSRF            string
	LoggedIn        bool
	IsOwner         bool
	OwnedGalleries  []galleryLink
	GalleryName     string
	GallerySlug     string
	WallColours     []choice
	InviteRoles     []choice
	InviteURL       string
	Lead            string
	AuthMode        string
	BeginURL        string
	FinishURL       string
}

type galleryLink struct {
	Slug string
	Name string
}

type choice struct {
	Value string
	Label string
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
	if m, ok := s.sessionMember(r); ok {
		data.LoggedIn = true
		owns, err := s.Index.MemberOwnsAny(m.ID)
		if err == nil && owns {
			data.IsOwner = true
		}
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
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	visible, err := s.Index.VisibleArtistSlugs(m.ID)
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery", data)
		return
	}
	rows, err := s.Index.ListArtworks()
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery", data)
		return
	}
	for _, row := range rows {
		if _, show := visible[row.ArtistSlug]; !show {
			continue
		}
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
	if data.IsOwner {
		owned, err := s.ownedGalleryLinks(m.ID)
		if err != nil {
			data.Error = data.T("error_generic")
		} else {
			data.OwnedGalleries = owned
		}
	}
	s.render(w, "gallery", data)
}

func (s *Server) galleryNewGET(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.ownsAny(w, r, m.ID) {
		return
	}
	data := s.page(w, r)
	data.Title = data.T("gallery_new_title")
	data.WallColours = wallColourOptions(data.Lang)
	s.render(w, "gallery_new", data)
}

func (s *Server) galleryNewPOST(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.ownsAny(w, r, m.ID) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	data := s.page(w, r)
	data.Title = data.T("gallery_new_title")
	data.WallColours = wallColourOptions(data.Lang)
	name := strings.TrimSpace(r.FormValue("name"))
	colour := strings.TrimSpace(r.FormValue("wall_colour"))
	if name == "" || !store.ValidWallColour(colour) {
		data.Error = data.T("error_required")
		s.render(w, "gallery_new", data)
		return
	}
	g, err := s.Store.CreateGallery(name, colour)
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery_new", data)
		return
	}
	if err := s.Store.AddGalleryMember(g.Slug, m.ID, store.RoleOwner); err != nil {
		_ = os.RemoveAll(filepath.Join(s.Store.GalleriesRoot(), g.Slug))
		data.Error = data.T("error_generic")
		s.render(w, "gallery_new", data)
		return
	}
	if err := s.Index.RebuildKeepingSessions(s.Store); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery_new", data)
		return
	}
	http.Redirect(w, r, "/galleries/"+g.Slug+"?lang="+data.Lang+"&flash=gallery_created", http.StatusSeeOther)
}

func (s *Server) galleryManageGET(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	slug := r.PathValue("gallery")
	if !s.ownsGallery(w, r, m.ID, slug) {
		return
	}
	data := s.page(w, r)
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = data.T(flash)
	}
	data, err := s.fillManage(data, m.ID, slug)
	if err != nil {
		data.Title = data.T("gallery_title")
		data.Error = data.T("error_generic")
		s.render(w, "gallery_manage", data)
		return
	}
	s.render(w, "gallery_manage", data)
}

func (s *Server) galleryArtistPOST(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	slug := r.PathValue("gallery")
	if !s.ownsGallery(w, r, m.ID, slug) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	data := s.page(w, r)
	data, err := s.fillManage(data, m.ID, slug)
	if err != nil {
		data.Title = data.T("gallery_title")
		data.Error = data.T("error_generic")
		s.render(w, "gallery_manage", data)
		return
	}
	artist := strings.TrimSpace(r.FormValue("artist_slug"))
	if artist == "" {
		data.Error = data.T("error_required")
		s.render(w, "gallery_manage", data)
		return
	}
	// An artist who only lives in a gallery this member does not own stays
	// there. Sharing one artist across families is left for H2.
	allowed, err := s.ownedArtistSet(m.ID)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return
	}
	if !allowed[artist] {
		http.NotFound(w, r)
		return
	}
	if err := s.Store.AddGalleryArtist(slug, artist); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery_manage", data)
		return
	}
	if err := s.Index.RebuildKeepingSessions(s.Store); err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery_manage", data)
		return
	}
	http.Redirect(w, r, "/galleries/"+slug+"?lang="+data.Lang+"&flash=gallery_artist_added", http.StatusSeeOther)
}

// galleryInvitePOST issues a one-time link. The owner check below is what
// TestOnlyOwnerOfThatGalleryCanIssueInvite fails on when it is removed.
func (s *Server) galleryInvitePOST(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	slug := r.PathValue("gallery")
	owns, err := s.Index.MemberOwnsGallery(m.ID, slug)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return
	}
	if !owns {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	data := s.page(w, r)
	data, err = s.fillManage(data, m.ID, slug)
	if err != nil {
		data.Title = data.T("gallery_title")
		data.Error = data.T("error_generic")
		s.render(w, "gallery_manage", data)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	role := r.FormValue("role")
	if name == "" {
		data.Error = data.T("error_required")
		s.render(w, "gallery_manage", data)
		return
	}
	if role != store.RoleContributor && role != store.RoleViewer {
		data.Error = data.T("error_invite_role")
		s.render(w, "gallery_manage", data)
		return
	}
	link, err := s.Store.CreateGalleryInvite(name, slug, role, s.cfg.Origin, time.Now())
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "gallery_manage", data)
		return
	}
	data.Title = data.T("invite_link_title")
	data.InviteURL = link
	s.render(w, "invite_link", data)
}

func (s *Server) artistNewGET(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.ownsAny(w, r, m.ID) {
		return
	}
	data := s.page(w, r)
	data.Title = data.T("artist_new_title")
	owned, err := s.ownedGalleryLinks(m.ID)
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artist_new", data)
		return
	}
	data.OwnedGalleries = owned
	want := r.URL.Query().Get("gallery")
	if want != "" {
		if !s.ownsGallery(w, r, m.ID, want) {
			return
		}
		data.SelectedGallery = want
	}
	s.render(w, "artist_new", data)
}

func (s *Server) artistNewPOST(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.ownsAny(w, r, m.ID) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	data := s.page(w, r)
	lang := data.Lang
	data.Title = data.T("artist_new_title")
	owned, err := s.ownedGalleryLinks(m.ID)
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artist_new", data)
		return
	}
	data.OwnedGalleries = owned
	gallerySlug := strings.TrimSpace(r.FormValue("gallery"))
	if gallerySlug == "" {
		data.Error = data.T("error_required")
		s.render(w, "artist_new", data)
		return
	}
	owns, err := s.Index.MemberOwnsGallery(m.ID, gallerySlug)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return
	}
	if !owns {
		http.NotFound(w, r)
		return
	}
	data.SelectedGallery = gallerySlug
	name := r.FormValue("name")
	birth, err := store.ParseDate(r.FormValue("birth_date"))
	if err != nil || strings.TrimSpace(name) == "" {
		data.Error = data.T("error_required")
		s.render(w, "artist_new", data)
		return
	}
	artist, err := s.Store.CreateArtist(name, birth.Time)
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artist_new", data)
		return
	}
	if err := s.Store.AddGalleryArtist(gallerySlug, artist.Slug); err != nil {
		_ = os.RemoveAll(filepath.Join(s.Store.ArtistsRoot(), artist.Slug))
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
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	can, err := s.Index.MemberCanUpload(m.ID)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return
	}
	if !can {
		http.NotFound(w, r)
		return
	}
	data := s.page(w, r)
	data.Title = data.T("upload_title")
	artists, err := s.uploadableArtists(m.ID)
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
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	canAny, err := s.Index.MemberCanUpload(m.ID)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return
	}
	if !canAny {
		http.NotFound(w, r)
		return
	}
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
	artists, err := s.uploadableArtists(m.ID)
	if err != nil {
		data.Error = data.T("error_generic")
		s.render(w, "artwork_new", data)
		return
	}
	data.Artists = artists
	artistSlug := r.FormValue("artist_slug")
	data.SelectedArtist = artistSlug
	if artistSlug != "" {
		can, err := s.Index.CanUploadArtist(m.ID, artistSlug)
		if err != nil {
			data.Error = data.T("error_generic")
			s.render(w, "artwork_new", data)
			return
		}
		if !can {
			http.NotFound(w, r)
			return
		}
	}
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
	if s.hiddenArtist(w, r, artist) {
		return
	}
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
	if s.hiddenArtist(w, r, r.PathValue("artist")) {
		return
	}
	rec, err := s.Store.LoadArtwork(r.PathValue("artist"), r.PathValue("folder"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, rec.OriginalPath)
}

func (s *Server) serveThumb(w http.ResponseWriter, r *http.Request) {
	if s.hiddenArtist(w, r, r.PathValue("artist")) {
		return
	}
	rec, err := s.Store.LoadArtwork(r.PathValue("artist"), r.PathValue("folder"))
	if err != nil || rec.ThumbPath == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, rec.ThumbPath)
}

// hiddenArtist writes 404 when the signed-in member shares no gallery with
// artistSlug, and 500 when the index cannot answer. It returns true when
// the response is already written.
func (s *Server) hiddenArtist(w http.ResponseWriter, r *http.Request, artistSlug string) bool {
	m, ok := s.sessionMember(r)
	if !ok {
		http.NotFound(w, r)
		return true
	}
	see, err := s.Index.CanSeeArtist(m.ID, artistSlug)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return true
	}
	if !see {
		http.NotFound(w, r)
		return true
	}
	return false
}

func (s *Server) ownsAny(w http.ResponseWriter, r *http.Request, memberID string) bool {
	owns, err := s.Index.MemberOwnsAny(memberID)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return false
	}
	if !owns {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (s *Server) ownsGallery(w http.ResponseWriter, r *http.Request, memberID, gallerySlug string) bool {
	owns, err := s.Index.MemberOwnsGallery(memberID, gallerySlug)
	if err != nil {
		http.Error(w, genericError(r), http.StatusInternalServerError)
		return false
	}
	if !owns {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (s *Server) ownedGalleryLinks(memberID string) ([]galleryLink, error) {
	rows, err := s.Index.OwnedGalleries(memberID)
	if err != nil {
		return nil, err
	}
	out := make([]galleryLink, 0, len(rows))
	for _, g := range rows {
		out = append(out, galleryLink{Slug: g.Slug, Name: g.Name})
	}
	return out, nil
}

func (s *Server) fillManage(data pageData, memberID, slug string) (pageData, error) {
	g, err := s.Store.LoadGallery(slug)
	if err != nil {
		return data, err
	}
	all, err := s.Store.ListArtists()
	if err != nil {
		return data, err
	}
	allowed, err := s.ownedArtistSet(memberID)
	if err != nil {
		return data, err
	}
	var mine []store.Artist
	for _, a := range all {
		if allowed[a.Slug] {
			mine = append(mine, a)
		}
	}
	data.GallerySlug = g.Slug
	data.GalleryName = g.Name
	data.Title = g.Name
	data.Artists = artistsNotIn(mine, g.Artists)
	data.InviteRoles = inviteRoleOptions(data.Lang)
	data.Lead = i18n.T(data.Lang, "gallery_wall_chosen", map[string]string{
		"colour": wallColourLabel(data.Lang, g.WallColour),
	})
	return data, nil
}

// ownedArtistSet is the artists already listed in a gallery this member owns.
// A brand-new artist is created through /artists/new, which attaches them
// to a gallery the member owns in the same request.
func (s *Server) ownedArtistSet(memberID string) (map[string]bool, error) {
	owned, err := s.Index.OwnedGalleries(memberID)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, g := range owned {
		full, err := s.Store.LoadGallery(g.Slug)
		if err != nil {
			return nil, err
		}
		for _, slug := range full.Artists {
			set[slug] = true
		}
	}
	return set, nil
}

func artistsNotIn(all []store.Artist, in []string) []store.Artist {
	have := map[string]bool{}
	for _, slug := range in {
		have[slug] = true
	}
	var out []store.Artist
	for _, a := range all {
		if !have[a.Slug] {
			out = append(out, a)
		}
	}
	return out
}

func wallColourOptions(lang string) []choice {
	out := make([]choice, 0, len(store.WallColours))
	for _, colour := range store.WallColours {
		out = append(out, choice{Value: colour, Label: wallColourLabel(lang, colour)})
	}
	return out
}

func wallColourLabel(lang, colour string) string {
	key := map[string]string{
		"off-white":  "wall_off_white",
		"warm-stone": "wall_warm_stone",
		"pale-sage":  "wall_pale_sage",
		"dusty-blue": "wall_dusty_blue",
		"soft-sand":  "wall_soft_sand",
	}[colour]
	if key == "" {
		return colour
	}
	return i18n.T(lang, key, nil)
}

func inviteRoleOptions(lang string) []choice {
	return []choice{
		{Value: store.RoleContributor, Label: i18n.T(lang, "role_contributor", nil)},
		{Value: store.RoleViewer, Label: i18n.T(lang, "role_viewer", nil)},
	}
}

func (s *Server) uploadableArtists(memberID string) ([]store.Artist, error) {
	slugs, err := s.Index.UploadableArtistSlugs(memberID)
	if err != nil {
		return nil, err
	}
	all, err := s.Store.ListArtists()
	if err != nil {
		return nil, err
	}
	var out []store.Artist
	for _, a := range all {
		if _, ok := slugs[a.Slug]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func genericError(r *http.Request) string {
	return i18n.T(i18n.NormalizeLang(r.URL.Query().Get("lang")), "error_generic", nil)
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

func (s *Server) manifestGET(w http.ResponseWriter, r *http.Request) {
	b, err := assets.ReadFile("static/manifest.webmanifest")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write(b)
}

func (s *Server) swGET(w http.ResponseWriter, r *http.Request) {
	b, err := assets.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}
