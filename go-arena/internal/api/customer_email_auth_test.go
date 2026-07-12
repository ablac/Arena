package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"arena-server/internal/db"

	"github.com/go-chi/chi/v5"
)

type fakeCustomerEmailStore struct {
	email       string
	displayName string
	returnTo    string
	tokenHash   []byte
	createdAt   time.Time
	ttl         time.Duration
	cooldown    time.Duration
	createErr   error
	consumeErr  error
	account     *db.CustomerAccount
	deleted     [][]byte
}

func (s *fakeCustomerEmailStore) CreateVerification(_ context.Context, email, displayName, returnTo string, tokenHash []byte, createdAt time.Time, ttl, cooldown time.Duration) error {
	s.email = email
	s.displayName = displayName
	s.returnTo = returnTo
	s.tokenHash = append([]byte(nil), tokenHash...)
	s.createdAt = createdAt
	s.ttl = ttl
	s.cooldown = cooldown
	return s.createErr
}

func (s *fakeCustomerEmailStore) ConsumeVerification(_ context.Context, tokenHash []byte, _ time.Time) (*db.CustomerAccount, string, error) {
	if s.consumeErr != nil {
		return nil, "", s.consumeErr
	}
	if !equalBytes(tokenHash, s.tokenHash) {
		return nil, "", db.ErrCustomerEmailVerificationInvalid
	}
	return s.account, s.returnTo, nil
}

func (s *fakeCustomerEmailStore) DeleteVerification(_ context.Context, tokenHash []byte) error {
	s.deleted = append(s.deleted, append([]byte(nil), tokenHash...))
	return nil
}

type fakeCustomerEmailSender struct {
	to          string
	displayName string
	magicLink   string
	expiresIn   time.Duration
	err         error
}

func (s *fakeCustomerEmailSender) SendMagicLink(_ context.Context, to, displayName, magicLink string, expiresIn time.Duration) error {
	s.to = to
	s.displayName = displayName
	s.magicLink = magicLink
	s.expiresIn = expiresIn
	return s.err
}

func newTestCustomerEmailHandler(store *fakeCustomerEmailStore, sender *fakeCustomerEmailSender) *CustomerOIDCHandler {
	return &CustomerOIDCHandler{
		sessions:          make(map[string]*CustomerSession),
		states:            make(map[string]customerOIDCTransaction),
		emailStore:        store,
		emailSender:       sender,
		emailSignInURL:    "https://arena.example/dashboard/",
		emailTokenTTL:     15 * time.Minute,
		emailSendCooldown: time.Minute,
	}
}

func emailAuthRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://arena.example")
	return req
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}

func magicTokenFromLink(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "arena.example" || parsed.Path != "/dashboard/" {
		t.Fatalf("unexpected magic-link destination: %s", raw)
	}
	values, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	token := values.Get("email_token")
	if token == "" {
		t.Fatalf("magic link has no fragment token: %s", raw)
	}
	if parsed.RawQuery != "" {
		t.Fatalf("magic token must not be placed in the server-visible query: %s", raw)
	}
	return token
}

func TestCustomerEmailStartStoresOnlyDigestAndSendsFragmentLink(t *testing.T) {
	store := &fakeCustomerEmailStore{}
	sender := &fakeCustomerEmailSender{}
	handler := newTestCustomerEmailHandler(store, sender)
	req := emailAuthRequest(http.MethodPost, "https://arena.example/api/v1/account/email/start", `{
		"email":" Pilot@Example.COM ",
		"display_name":"  Pilot One  ",
		"return_to":"/dashboard/?tab=cosmetics"
	}`)
	rec := httptest.NewRecorder()

	handler.EmailStartHandler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.email != "pilot@example.com" || store.displayName != "Pilot One" || store.returnTo != "/dashboard/?tab=cosmetics" {
		t.Fatalf("stored verification = email=%q name=%q return=%q", store.email, store.displayName, store.returnTo)
	}
	if len(store.tokenHash) != sha256.Size {
		t.Fatalf("stored token digest length = %d", len(store.tokenHash))
	}
	if sender.to != store.email || sender.displayName != store.displayName || sender.expiresIn != 15*time.Minute {
		t.Fatalf("mail request = %+v", sender)
	}
	rawToken := magicTokenFromLink(t, sender.magicLink)
	wantDigest := sha256.Sum256([]byte(rawToken))
	if !equalBytes(store.tokenHash, wantDigest[:]) {
		t.Fatal("database claim is not the SHA-256 digest of the fragment-only token")
	}
	if strings.Contains(rec.Body.String(), store.email) || strings.Contains(rec.Body.String(), rawToken) {
		t.Fatalf("generic response leaked identity or token: %s", rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCustomerEmailStartIsSameOriginBoundAndRollsBackFailedDelivery(t *testing.T) {
	store := &fakeCustomerEmailStore{}
	sender := &fakeCustomerEmailSender{err: errors.New("smtp offline")}
	handler := newTestCustomerEmailHandler(store, sender)

	crossOrigin := emailAuthRequest(http.MethodPost, "https://arena.example/api/v1/account/email/start", `{"email":"pilot@example.com"}`)
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	crossRec := httptest.NewRecorder()
	handler.EmailStartHandler(crossRec, crossOrigin)
	if crossRec.Code != http.StatusForbidden || sender.magicLink != "" {
		t.Fatalf("cross-origin start = %d, mail=%q", crossRec.Code, sender.magicLink)
	}

	req := emailAuthRequest(http.MethodPost, "https://arena.example/api/v1/account/email/start", `{"email":"pilot@example.com"}`)
	rec := httptest.NewRecorder()
	handler.EmailStartHandler(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("delivery failure status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(store.deleted) != 1 || !equalBytes(store.deleted[0], store.tokenHash) {
		t.Fatal("failed delivery did not delete the unusable verification claim")
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "smtp") {
		t.Fatalf("delivery response leaked provider detail: %s", rec.Body.String())
	}
}

func TestCustomerEmailDeliveryFailureDoesNotLogRecipientOrSMTPDetail(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	store := &fakeCustomerEmailStore{}
	sender := &fakeCustomerEmailSender{err: errors.New("550 recipient pilot@example.com rejected by smtp.internal")}
	handler := newTestCustomerEmailHandler(store, sender)
	req := emailAuthRequest(http.MethodPost, "https://arena.example/api/v1/account/email/start", `{"email":"pilot@example.com"}`)
	handler.EmailStartHandler(httptest.NewRecorder(), req)
	logged := strings.ToLower(logs.String())
	if strings.Contains(logged, "pilot@example.com") || strings.Contains(logged, "smtp.internal") || strings.Contains(logged, "recipient") {
		t.Fatalf("customer delivery log leaked provider or recipient detail: %s", logged)
	}
}

func TestCustomerEmailVerifyCreatesSecureVerifiedSession(t *testing.T) {
	verifiedAt := time.Now().UTC()
	store := &fakeCustomerEmailStore{
		account: &db.CustomerAccount{
			ID: "account-1", Email: "pilot@example.com", DisplayName: "Pilot One", EmailVerifiedAt: &verifiedAt,
		},
		returnTo: "/dashboard/?tab=cosmetics",
	}
	sender := &fakeCustomerEmailSender{}
	handler := newTestCustomerEmailHandler(store, sender)

	start := emailAuthRequest(http.MethodPost, "https://arena.example/api/v1/account/email/start", `{"email":"pilot@example.com","return_to":"/dashboard/?tab=cosmetics"}`)
	handler.EmailStartHandler(httptest.NewRecorder(), start)
	rawToken := magicTokenFromLink(t, sender.magicLink)

	verify := emailAuthRequest(http.MethodPost, "https://arena.example/api/v1/account/email/verify", `{"token":"`+rawToken+`"}`)
	rec := httptest.NewRecorder()
	handler.EmailVerifyHandler(rec, verify)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Verified   bool   `json:"verified"`
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Verified || payload.RedirectTo != store.returnTo {
		t.Fatalf("verify payload = %+v", payload)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == customerSessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v", sessionCookie)
	}
	sessionReq := httptest.NewRequest(http.MethodGet, "https://arena.example/api/v1/account/session", nil)
	sessionReq.AddCookie(sessionCookie)
	session := handler.GetSession(sessionReq)
	if session == nil || session.AccountID != store.account.ID || session.Email != store.account.Email || session.EmailVerifiedAt == nil || session.CSRFToken == "" {
		t.Fatalf("verified session = %+v", session)
	}
}

func TestCustomerEmailVerifyRejectsInvalidTokenWithoutSession(t *testing.T) {
	store := &fakeCustomerEmailStore{consumeErr: db.ErrCustomerEmailVerificationInvalid}
	handler := newTestCustomerEmailHandler(store, &fakeCustomerEmailSender{})
	req := emailAuthRequest(http.MethodPost, "https://arena.example/api/v1/account/email/verify", `{"token":"expired-token"}`)
	rec := httptest.NewRecorder()
	handler.EmailVerifyHandler(rec, req)
	if rec.Code != http.StatusBadRequest || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("invalid verify = %d cookies=%+v body=%s", rec.Code, rec.Result().Cookies(), rec.Body.String())
	}
}

func TestCustomerEmailRoutesAreMountedAndStartFailsClosedWithoutRedis(t *testing.T) {
	store := &fakeCustomerEmailStore{}
	sender := &fakeCustomerEmailSender{}
	handler := newTestCustomerEmailHandler(store, sender)
	router := chi.NewRouter()
	router.Route("/api/v1", func(api chi.Router) { registerCustomerEmailAuthRoutes(api, handler) })
	router.Route("/arena/api/v1", func(api chi.Router) { registerCustomerEmailAuthRoutes(api, handler) })

	for _, prefix := range []string{"/api/v1", "/arena/api/v1"} {
		start := emailAuthRequest(http.MethodPost, "https://arena.example"+prefix+"/account/email/start", `{"email":"pilot@example.com","return_to":"/dashboard/"}`)
		startRec := httptest.NewRecorder()
		router.ServeHTTP(startRec, start)
		if startRec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s start status = %d, body=%s", prefix, startRec.Code, startRec.Body.String())
		}
		if sender.magicLink != "" || store.email != "" {
			t.Fatalf("%s start reached email handler without a working limiter", prefix)
		}

		verify := emailAuthRequest(http.MethodPost, "https://arena.example"+prefix+"/account/email/verify", `{"token":"a-valid-length-token-that-was-never-issued"}`)
		verifyRec := httptest.NewRecorder()
		router.ServeHTTP(verifyRec, verify)
		if verifyRec.Code != http.StatusBadRequest {
			t.Fatalf("%s verify status = %d, body=%s", prefix, verifyRec.Code, verifyRec.Body.String())
		}
	}
}

func TestCustomerEmailSessionCapabilityAdvertisesMountAwareEndpoints(t *testing.T) {
	handler := newTestCustomerEmailHandler(&fakeCustomerEmailStore{}, &fakeCustomerEmailSender{})
	for _, test := range []struct {
		path       string
		wantPrefix string
	}{
		{path: "https://arena.example/api/v1/account/session", wantPrefix: "/api/v1"},
		{path: "https://arena.example/arena/api/v1/account/session", wantPrefix: "/arena/api/v1"},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		rec := httptest.NewRecorder()
		handler.SessionInfoHandler(rec, req)
		var payload struct {
			LoginEnabled      bool   `json:"login_enabled"`
			EmailLoginEnabled bool   `json:"email_login_enabled"`
			OIDCLoginEnabled  bool   `json:"oidc_login_enabled"`
			EmailStartURL     string `json:"email_start_url"`
			EmailVerifyURL    string `json:"email_verify_url"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if !payload.LoginEnabled || !payload.EmailLoginEnabled || payload.OIDCLoginEnabled ||
			payload.EmailStartURL != test.wantPrefix+"/account/email/start" ||
			payload.EmailVerifyURL != test.wantPrefix+"/account/email/verify" {
			t.Fatalf("%s capability = %+v", test.path, payload)
		}
	}
}
