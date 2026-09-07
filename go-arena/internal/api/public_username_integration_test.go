package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"arena-server/internal/db"
	"arena-server/internal/platform"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func useUsernamePostgres(t *testing.T) {
	t.Helper()
	url := os.Getenv("ARENA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set ARENA_TEST_DATABASE_URL for public username integration")
	}
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "arena_username_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior := db.Pool
	db.Pool = pool
	t.Cleanup(func() {
		db.Pool = prior
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err := db.EnsureCoreSchema(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresUsernameRefreshPreservesOldAdminCookies(t *testing.T) {
	useUsernamePostgres(t)
	accounts := newAngelAccounts(t)
	handler, _ := newArenaSignedInWithAngel(t, accounts)
	handler.authority = platform.PostgresAuthority{}
	original, cookie := signInThroughAngel(t, handler, accounts, map[string]any{"name": "Private Real Name", "preferred_username": "original_pilot", "product_admin": true})
	req := httptest.NewRequest(http.MethodGet, "https://arena.example/api/v1/account/session", nil)
	req.AddCookie(cookie)
	// A new sign-in changes only the durable central alias. Earlier cookies keep
	// their own time-bounded grant, CSRF token and account identity.
	for _, alias := range []any{"renamed_pilot", nil} {
		claims := map[string]any{"name": "Private Real Name"}
		if alias != nil {
			claims["preferred_username"] = alias
		}
		fresh, _ := signInThroughAngel(t, handler, accounts, claims)
		if fresh.AccountID != original.AccountID {
			t.Fatal("rename/removal changed account ownership")
		}
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 8; j++ {
					got := handler.GetSession(req)
					if got == nil || got.AccountID != original.AccountID || got.CSRFToken != original.CSRFToken || got.platformAdmin != original.platformAdmin {
						t.Error("refresh replaced original session authority")
						return
					}
					if _, ok := got.platformAdminGrantAt(time.Now()); !ok {
						t.Error("administrator grant lost")
					}
					if (alias == nil && got.PublicUsername != nil) || (alias != nil && (got.PublicUsername == nil || *got.PublicUsername != alias)) {
						t.Error("old cookie alias is stale")
					}
				}
			}()
		}
		wg.Wait()
		restored := newTestCustomerOIDCHandler().GetSession(req)
		if restored == nil || restored.CSRFToken != original.CSRFToken {
			t.Fatal("null email prevented durable session restore")
		}
		if _, ok := restored.platformAdminGrantAt(time.Now()); ok {
			t.Fatal("durable restore invented an admin grant")
		}
		rec := httptest.NewRecorder()
		handler.SessionInfoHandler(rec, req)
		if strings.Contains(rec.Body.String(), "Private Real Name") || strings.Contains(rec.Body.String(), "original_pilot") {
			t.Fatalf("session exposed stale/private label: %s", rec.Body.String())
		}
		profileReq := httptest.NewRequest(http.MethodGet, "/api/v1/profile/"+original.AccountID, nil)
		route := chi.NewRouteContext()
		route.URLParams.Add("account_id", original.AccountID)
		profileReq = profileReq.WithContext(context.WithValue(profileReq.Context(), chi.RouteCtxKey, route))
		profileRec := httptest.NewRecorder()
		PublicProfileHandler(profileRec, profileReq)
		if profileRec.Code != 200 || strings.Contains(profileRec.Body.String(), "Private Real Name") {
			t.Fatalf("public profile=%d %s", profileRec.Code, profileRec.Body.String())
		}
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/v1/account/profile", strings.NewReader(`{"bio":"A public bio","avatar_color":"#abc","show_bots_public":false}`))
	patch = patch.WithContext(withCustomerSession(patch.Context(), handler.GetSession(req)))
	rec := httptest.NewRecorder()
	UpdateAccountProfileHandler(rec, patch)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "A public bio") {
		t.Fatalf("cosmetic profile edit=%d %s", rec.Code, rec.Body.String())
	}
	if handler.GetSession(req).platformAdmin != original.platformAdmin {
		t.Fatal("profile edit evicted administrator session")
	}
}
