package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/pserrano95/pinacoteca/internal/i18n"
	"github.com/pserrano95/pinacoteca/internal/store"
)

const (
	sessionCookie  = "pinacoteca_session"
	csrfCookie     = "pinacoteca_csrf"
	ceremonyCookie = "pinacoteca_ceremony"
	sessionTTL     = 30 * 24 * time.Hour
	csrfTTL        = 30 * 24 * time.Hour
	ceremonyTTL    = 5 * time.Minute
)

// Config is the WebAuthn relying party for this process.
type Config struct {
	RPID          string
	Origin        string
	RPDisplayName string
}

// ConfigFromBase derives the relying party id and origin from baseURL.
// Explicit rpID and origin replace the derived values when they are not empty.
func ConfigFromBase(baseURL, rpID, origin string) (Config, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Config{}, fmt.Errorf("invalid base url %q", baseURL)
	}
	if strings.TrimSpace(origin) == "" {
		origin = u.Scheme + "://" + u.Host
	}
	ou, err := url.Parse(strings.TrimRight(strings.TrimSpace(origin), "/"))
	if err != nil || ou.Host == "" || (ou.Scheme != "http" && ou.Scheme != "https") {
		return Config{}, fmt.Errorf("invalid origin %q", origin)
	}
	origin = ou.Scheme + "://" + ou.Host
	if strings.TrimSpace(rpID) == "" {
		rpID = u.Hostname()
	}
	if rpID == "" {
		return Config{}, fmt.Errorf("relying party id is empty")
	}
	return Config{
		RPID:          rpID,
		Origin:        origin,
		RPDisplayName: i18n.T("en", "app_name", nil),
	}, nil
}

type ceremony struct {
	Kind    string
	Token   string
	Session webauthn.SessionData
	Expires time.Time
}

type routeSpec struct {
	Method  string
	Pattern string
	Public  bool
	Fail    string
}

// routeSpecs is the only list of routes the server registers.
// Public routes are the ones the issue leaves open. Everything else requires
// a session: pages redirect to /login, other requests answer 401.
func (s *Server) routeSpecs() []routeSpec {
	return []routeSpec{
		{http.MethodGet, "/static/", true, ""},
		{http.MethodGet, "/login", true, ""},
		{http.MethodGet, "/invite/{token}", true, ""},
		{http.MethodPost, "/login/begin", true, ""},
		{http.MethodPost, "/login/finish", true, ""},
		{http.MethodPost, "/invite/{token}/begin", true, ""},
		{http.MethodPost, "/invite/{token}/finish", true, ""},
		{http.MethodGet, "/{$}", false, "redirect"},
		{http.MethodGet, "/artists/new", false, "redirect"},
		{http.MethodPost, "/artists/new", false, "unauthorized"},
		{http.MethodGet, "/artworks/new", false, "redirect"},
		{http.MethodPost, "/artworks/new", false, "unauthorized"},
		{http.MethodGet, "/artists/{artist}/artworks/{folder}", false, "redirect"},
		{http.MethodGet, "/artists/{artist}/artworks/{folder}/original", false, "unauthorized"},
		{http.MethodGet, "/artists/{artist}/artworks/{folder}/thumb", false, "unauthorized"},
		{http.MethodPost, "/logout", false, "unauthorized"},
	}
}

func (s *Server) withSession(next http.Handler, fail string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.sessionMember(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		lang := i18n.NormalizeLang(r.URL.Query().Get("lang"))
		if fail == "redirect" {
			http.Redirect(w, r, "/login?lang="+lang, http.StatusSeeOther)
			return
		}
		http.Error(w, i18n.T(lang, "error_unauthorized", nil), http.StatusUnauthorized)
	})
}

func (s *Server) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.validCSRF(r) {
			lang := i18n.NormalizeLang(r.URL.Query().Get("lang"))
			http.Error(w, i18n.T(lang, "error_csrf", nil), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	if err != nil || c.Value == "" {
		return false
	}
	got := r.Header.Get("X-CSRF-Token")
	if got == "" {
		ct := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "multipart/form-data"):
			if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
				return false
			}
		default:
			if err := r.ParseForm(); err != nil {
				return false
			}
		}
		got = r.FormValue("csrf")
	}
	if len(got) != len(c.Value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(c.Value)) == 1
}

func (s *Server) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil && c.Value != "" {
		return c.Value
	}
	tok, err := randomToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, s.cookie(csrfCookie, tok, csrfTTL))
	return tok
}

func (s *Server) sessionMember(r *http.Request) (store.Member, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return store.Member{}, false
	}
	m, err := s.Index.MemberFromSession(c.Value, time.Now())
	if err != nil {
		return store.Member{}, false
	}
	return m, true
}

func (s *Server) startSession(w http.ResponseWriter, memberID string) error {
	id, err := randomToken()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.Index.CreateSession(id, memberID, now, now.Add(sessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, s.cookie(sessionCookie, id, sessionTTL))
	return nil
}

func (s *Server) cookie(name, value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure,
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl),
	}
}

func (s *Server) loginGET(w http.ResponseWriter, r *http.Request) {
	lang := i18n.NormalizeLang(langFrom(r))
	if _, ok := s.sessionMember(r); ok {
		http.Redirect(w, r, "/?lang="+lang, http.StatusSeeOther)
		return
	}
	data := s.page(w, r)
	data.LoggedIn = false
	data.Title = data.T("login_title")
	data.AuthMode = "login"
	data.BeginURL = "/login/begin?lang=" + data.Lang
	data.FinishURL = "/login/finish?lang=" + data.Lang
	s.render(w, "login", data)
}

func (s *Server) loginBegin(w http.ResponseWriter, r *http.Request) {
	assertion, session, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	id, err := s.putCeremony(ceremony{Kind: "login", Session: *session})
	if err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, s.cookie(ceremonyCookie, id, ceremonyTTL))
	s.writeJSON(w, http.StatusOK, assertion)
}

func (s *Server) loginFinish(w http.ResponseWriter, r *http.Request) {
	cer, ok := s.takeCeremony(r, "login", "")
	if !ok {
		s.authFail(w, r, http.StatusBadRequest)
		return
	}
	user, cred, err := s.wa.FinishPasskeyLogin(s.discoverUser, cer.Session, r)
	if err != nil || cred == nil || cred.Authenticator.CloneWarning {
		s.authFail(w, r, http.StatusBadRequest)
		return
	}
	pk := passkeyFromCredential(cred, time.Now())
	memberID := hex.EncodeToString(user.WebAuthnID())
	if err := s.Store.UpdatePasskey(memberID, pk); err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	if err := s.Index.UpdatePasskey(pk); err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	if err := s.startSession(w, memberID); err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	lang := i18n.NormalizeLang(r.URL.Query().Get("lang"))
	s.writeJSON(w, http.StatusOK, map[string]string{"redirect": "/?lang=" + lang})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.Index.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure,
	})
	lang := i18n.NormalizeLang(r.FormValue("lang"))
	http.Redirect(w, r, "/login?lang="+lang, http.StatusSeeOther)
}

func (s *Server) inviteGET(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := s.Store.LookupInvite(token, time.Now())
	if err != nil {
		s.inviteInvalid(w, r)
		return
	}
	data := s.page(w, r)
	data.Title = data.T("invite_title")
	data.Lead = i18n.T(data.Lang, "invite_lead", map[string]string{"name": inv.Name})
	data.AuthMode = "register"
	data.BeginURL = "/invite/" + token + "/begin?lang=" + data.Lang
	data.FinishURL = "/invite/" + token + "/finish?lang=" + data.Lang
	s.render(w, "invite", data)
}

func (s *Server) inviteInvalid(w http.ResponseWriter, r *http.Request) {
	data := s.page(w, r)
	data.LoggedIn = false
	data.Title = data.T("invite_invalid_title")
	s.renderStatus(w, http.StatusNotFound, "invite_invalid", data)
}

func (s *Server) inviteBegin(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := s.Store.LookupInvite(token, time.Now())
	if err != nil {
		s.authFail(w, r, http.StatusBadRequest)
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	user := &waUser{id: raw, name: inv.Name}
	creation, session, err := s.wa.BeginRegistration(user)
	if err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	id, err := s.putCeremony(ceremony{Kind: "register", Token: token, Session: *session})
	if err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, s.cookie(ceremonyCookie, id, ceremonyTTL))
	s.writeJSON(w, http.StatusOK, creation)
}

func (s *Server) inviteFinish(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	cer, ok := s.takeCeremony(r, "register", token)
	if !ok {
		s.authFail(w, r, http.StatusBadRequest)
		return
	}
	s.redeemMu.Lock()
	defer s.redeemMu.Unlock()
	inv, err := s.Store.LookupInvite(token, time.Now())
	if err != nil {
		s.authFail(w, r, http.StatusBadRequest)
		return
	}
	user := &waUser{id: append([]byte(nil), cer.Session.UserID...), name: inv.Name}
	cred, err := s.wa.FinishRegistration(user, cer.Session, r)
	if err != nil {
		s.authFail(w, r, http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	member := store.Member{
		SchemaVersion: store.SchemaVersion,
		ID:            hex.EncodeToString(cer.Session.UserID),
		Name:          inv.Name,
		CreatedAt:     now,
	}
	if err := s.Store.SaveMember(member, []store.Passkey{passkeyFromCredential(cred, now)}); err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	if err := s.Store.MarkInviteUsed(token, now); err != nil {
		_ = os.RemoveAll(filepath.Join(s.Store.MembersRoot(), member.ID))
		s.authFail(w, r, http.StatusBadRequest)
		return
	}
	if err := s.Index.RebuildKeepingSessions(s.Store); err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	if err := s.startSession(w, member.ID); err != nil {
		s.authFail(w, r, http.StatusInternalServerError)
		return
	}
	lang := i18n.NormalizeLang(r.URL.Query().Get("lang"))
	s.writeJSON(w, http.StatusOK, map[string]string{"redirect": "/?lang=" + lang})
}

func (s *Server) discoverUser(rawID, userHandle []byte) (webauthn.User, error) {
	credID := base64.RawURLEncoding.EncodeToString(rawID)
	m, pk, err := s.Index.FindPasskey(credID)
	if err != nil {
		return nil, err
	}
	id, err := hex.DecodeString(m.ID)
	if err != nil || subtle.ConstantTimeCompare(id, userHandle) != 1 {
		return nil, fmt.Errorf("user handle mismatch")
	}
	waCred, err := credentialFromPasskey(pk)
	if err != nil {
		return nil, err
	}
	return &waUser{id: id, name: m.Name, creds: []webauthn.Credential{waCred}}, nil
}

func (s *Server) putCeremony(c ceremony) (string, error) {
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	c.Expires = time.Now().Add(ceremonyTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ceremonies == nil {
		s.ceremonies = map[string]ceremony{}
	}
	s.ceremonies[id] = c
	return id, nil
}

func (s *Server) takeCeremony(r *http.Request, kind, token string) (ceremony, bool) {
	ck, err := r.Cookie(ceremonyCookie)
	if err != nil || ck.Value == "" {
		return ceremony{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.ceremonies[ck.Value]
	if !ok {
		return ceremony{}, false
	}
	delete(s.ceremonies, ck.Value)
	if time.Now().After(item.Expires) || item.Kind != kind || (token != "" && item.Token != token) {
		return ceremony{}, false
	}
	return item, true
}

func (s *Server) authFail(w http.ResponseWriter, r *http.Request, status int) {
	lang := i18n.NormalizeLang(r.URL.Query().Get("lang"))
	s.writeJSON(w, status, map[string]string{"error": i18n.T(lang, "auth_failed", nil)})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type waUser struct {
	id    []byte
	name  string
	creds []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.id }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.name }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func passkeyFromCredential(c *webauthn.Credential, created time.Time) store.Passkey {
	return store.Passkey{
		ID:              base64.RawURLEncoding.EncodeToString(c.ID),
		PublicKey:       base64.RawURLEncoding.EncodeToString(c.PublicKey),
		Counter:         c.Authenticator.SignCount,
		CreatedAt:       created.UTC().Truncate(time.Second),
		AAGUID:          base64.RawURLEncoding.EncodeToString(c.Authenticator.AAGUID),
		BackupEligible:  c.Flags.BackupEligible,
		BackupState:     c.Flags.BackupState,
		UserPresent:     c.Flags.UserPresent,
		UserVerified:    c.Flags.UserVerified,
		AttestationType: c.AttestationType,
	}
}

func newWebAuthn(cfg Config) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:                  cfg.RPID,
		RPDisplayName:         cfg.RPDisplayName,
		RPOrigins:             []string{cfg.Origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationPreferred,
		},
	})
}

func credentialFromPasskey(pk store.Passkey) (webauthn.Credential, error) {
	id, err := base64.RawURLEncoding.DecodeString(pk.ID)
	if err != nil {
		return webauthn.Credential{}, err
	}
	pub, err := base64.RawURLEncoding.DecodeString(pk.PublicKey)
	if err != nil {
		return webauthn.Credential{}, err
	}
	var aaguid []byte
	if pk.AAGUID != "" {
		aaguid, err = base64.RawURLEncoding.DecodeString(pk.AAGUID)
		if err != nil {
			return webauthn.Credential{}, err
		}
	}
	return webauthn.Credential{
		ID:              id,
		PublicKey:       pub,
		AttestationType: pk.AttestationType,
		Flags: webauthn.CredentialFlags{
			UserPresent:    pk.UserPresent,
			UserVerified:   pk.UserVerified,
			BackupEligible: pk.BackupEligible,
			BackupState:    pk.BackupState,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    aaguid,
			SignCount: pk.Counter,
		},
	}, nil
}
