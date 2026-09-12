package api

import (
	"arena-server/internal/accounts"
	"arena-server/internal/db"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIncludedBaseSyncRemovesPaidCosmeticsUntilUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name, upgrade string
		want          bool
	}{
		{"base removes old paid cache", `null`, false},
		{"paid preserves cosmetics", `{"source":"subscription","productSlug":"arena","planSlug":"all-access","active":true}`, true},
		{"expired removes paid cache", `{"source":"subscription","productSlug":"arena","planSlug":"all-access","active":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"entitlements":[{"source":"included","productSlug":"arena","active":true,"planSlug":null,"upgrade":` + tc.upgrade + `}]}`))
			}))
			defer server.Close()
			authority := &fakeIdentityAuthority{account: &db.CustomerAccount{ID: "owner", SubscriptionActive: true}, subscriptionBots: []string{"owned-bot"}}
			h := newTestCustomerOIDCHandler()
			h.authority = authority
			h.entitlements = accounts.NewClient(server.URL, server.Client())
			refreshed := false
			h.onSubscriptionSynced = func(context.Context, []string) { refreshed = true }
			change, err := h.syncEntitlementsFromAccounts(context.Background(), "owner", "transient-token")
			if err != nil || change == nil {
				t.Fatal(err)
			}
			if authority.account.SubscriptionActive != tc.want || len(authority.subscriptionCalls) != 1 {
				t.Fatal("wrong paid cache state")
			}
			if !tc.want && !refreshed {
				t.Fatal("connected bots kept stale paid presentation")
			}
		})
	}
}
