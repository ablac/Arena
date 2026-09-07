package accounts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func catalogStub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/catalog" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPlanSourceQuotesTheProductsPublicPaidPlan(t *testing.T) {
	server := catalogStub(t, http.StatusOK, `{"products":[
		{"id":"reef","slug":"agentreef","plans":[{"slug":"pro","priceCents":14900,"interval":"month","public":true}]},
		{"id":"arena","slug":"arena","plans":[{"slug":"all-access","priceCents":999,"interval":"month","public":true}]}
	]}`)
	source := NewPlanSource(server.URL, "arena", server.Client())
	if err := source.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	plan, ok := source.Get()
	if !ok {
		t.Fatal("the plan should be known after a successful read")
	}
	if plan.PriceCents != 999 || plan.Interval != "month" || plan.Slug != "all-access" {
		t.Fatalf("plan = %+v", plan)
	}
	// Another product's plan must never be quoted as Arena's.
	if plan.PriceCents == 14900 {
		t.Fatal("quoted a different product's price")
	}
	if plan.Currency != "USD" {
		t.Fatalf("currency = %q, want the USD default when the catalog names none", plan.Currency)
	}
}

// A free tier is not what "subscribe" costs, and a private plan is not on
// offer to the person reading the Shop.
func TestPlanSourceIgnoresFreeAndNonPublicPlans(t *testing.T) {
	server := catalogStub(t, http.StatusOK, `{"products":[{"id":"arena","plans":[
		{"slug":"free","priceCents":0,"interval":"month","public":true},
		{"slug":"secret","priceCents":100,"interval":"month","public":false},
		{"slug":"all-access","priceCents":999,"interval":"month","public":true},
		{"slug":"premium","priceCents":4900,"interval":"month","public":true}
	]}]}`)
	source := NewPlanSource(server.URL, "arena", server.Client())
	if err := source.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	plan, _ := source.Get()
	if plan.Slug != "all-access" || plan.PriceCents != 999 {
		t.Fatalf("plan = %+v, want the cheapest public paid plan", plan)
	}
}

// The Shop quotes nothing rather than guessing, and the catalog it serves must
// not fail because another service did.
func TestPlanSourceUnknownUntilAReadSucceeds(t *testing.T) {
	server := catalogStub(t, http.StatusInternalServerError, `nope`)
	source := NewPlanSource(server.URL, "arena", server.Client())
	if err := source.Refresh(context.Background()); err == nil {
		t.Fatal("a 500 should be reported to the caller")
	}
	if _, ok := source.Get(); ok {
		t.Fatal("no price may be reported when no read has succeeded")
	}
}

func TestPlanSourceWithdrawsThePriceWhenAReadFails(t *testing.T) {
	good := catalogStub(t, http.StatusOK,
		`{"products":[{"id":"arena","plans":[{"slug":"all-access","priceCents":999,"interval":"month","public":true}]}]}`)
	source := NewPlanSource(good.URL, "arena", good.Client())
	if err := source.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// A failed read cannot leave a previous price or sale available to quote.
	source.catalogURL = good.URL + "/gone"
	_ = source.Refresh(context.Background())
	plan, ok := source.Get()
	if ok || plan != (Plan{}) {
		t.Fatalf("plan = %+v ok=%v, want no available quote after a failed read", plan, ok)
	}
}

func TestPlanSourceIsInertWithoutAnIssuerOrProduct(t *testing.T) {
	for _, tc := range []struct{ issuer, product string }{
		{"", "arena"}, {"https://accounts.example", ""}, {"  ", "  "},
	} {
		source := NewPlanSource(tc.issuer, tc.product, nil)
		if source != nil {
			t.Fatalf("NewPlanSource(%q,%q) should be nil", tc.issuer, tc.product)
		}
		// And a nil source answers, rather than panicking, on every path.
		if _, ok := source.Get(); ok {
			t.Fatal("a nil source must report no price")
		}
		if err := source.Refresh(context.Background()); err != nil {
			t.Fatalf("a nil source must refresh to nothing, got %v", err)
		}
	}
}

func TestPlanSourceReportsAMissingProduct(t *testing.T) {
	server := catalogStub(t, http.StatusOK, `{"products":[{"id":"reef","plans":[]}]}`)
	source := NewPlanSource(server.URL, "arena", server.Client())
	if err := source.Refresh(context.Background()); err == nil {
		t.Fatal("a catalog without the product should be reported")
	}
	if _, ok := source.Get(); ok {
		t.Fatal("and must not leave a price behind")
	}
}

func TestPlanSourceCarriesCommercialTermsWithoutApplyingTheDiscount(t *testing.T) {
	server := catalogStub(t, http.StatusOK, `{"products":[{"id":"arena","plans":[{
		"slug":"all-access","priceCents":999,"interval":"month","public":true,
		"priceRevision":4,"seatsIncluded":3,
		"sale":{"id":"sale-1","name":"Launch","percentOff":null,"amountOffCents":500,
			"duration":"repeating","durationMonths":2,"startsAt":"2026-09-05T12:00:00Z","endsAt":"2026-09-06T12:00:00Z"}
	}]}]}`)
	source := NewPlanSource(server.URL, "arena", server.Client())
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	source.now = func() time.Time { return now }
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	plan, ok := source.Get()
	if !ok || plan.PriceRevision != 4 || plan.SeatsIncluded != 3 || plan.PriceCents != 999 {
		t.Fatalf("commercial terms = %+v, available=%v", plan, ok)
	}
	if plan.Sale == nil || plan.Sale.ID != "sale-1" || plan.Sale.PercentOff != nil || plan.Sale.AmountOffCents == nil || *plan.Sale.AmountOffCents != 500 || plan.Sale.DurationMonths == nil || *plan.Sale.DurationMonths != 2 {
		t.Fatalf("sale = %+v; the fixed discount is a subscription offer, not a per-seat reduction", plan.Sale)
	}
	if !plan.ValidUntil.Equal(now.Add(45 * time.Second)) {
		t.Fatalf("valid until = %v", plan.ValidUntil)
	}
	now = now.Add(45 * time.Second)
	if _, ok := source.Get(); ok {
		t.Fatal("an unrefreshed price must stop being quoted after 45 seconds")
	}
}

func TestPlanSourceExpiresAtSaleBoundariesAndRemovesEndedOffers(t *testing.T) {
	server := catalogStub(t, http.StatusOK, `{"products":[{"id":"arena","plans":[{
		"slug":"all-access","priceCents":999,"interval":"month","public":true,
		"sale":{"id":"sale-1","name":"Launch","percentOff":25,"amountOffCents":null,
			"duration":"once","durationMonths":null,"startsAt":"2026-09-05T12:00:10Z","endsAt":"2026-09-05T12:00:20Z"}
	}]}]}`)
	source := NewPlanSource(server.URL, "arena", server.Client())
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	source.now = func() time.Time { return now }
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	plan, ok := source.Get()
	if !ok || !plan.ValidUntil.Equal(now.Add(10*time.Second)) || source.NextRefresh(30*time.Second) != 10*time.Second {
		t.Fatalf("upcoming sale must trigger an early refresh: %+v", plan)
	}
	now = now.Add(10 * time.Second)
	if _, ok := source.Get(); ok {
		t.Fatal("a boundary requires confirmation from Accounts")
	}
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	plan, ok = source.Get()
	if !ok || plan.Sale == nil || !plan.ValidUntil.Equal(now.Add(10*time.Second)) {
		t.Fatalf("active sale = %+v, available=%v", plan, ok)
	}
	now = now.Add(10 * time.Second)
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	plan, ok = source.Get()
	if !ok || plan.Sale != nil || plan.PriceCents != 999 {
		t.Fatalf("ended offer must not survive a successful read: %+v", plan)
	}
}

func TestPlanSourceRejectsIncompleteOrContradictoryCommercialTerms(t *testing.T) {
	for _, terms := range []string{
		`"seatsIncluded":0`,
		`"currency":"EUR"`,
		`"priceRevision":-1`,
		`"sale":{"id":"x","name":"Offer","percentOff":10,"amountOffCents":100,"duration":"once"}`,
		`"sale":{"id":"x","name":"Offer","percentOff":10,"duration":"repeating","durationMonths":null}`,
		`"sale":{"id":"x","name":"Offer","percentOff":10,"duration":"once","startsAt":"2026-09-06T00:00:00Z","endsAt":"2026-09-05T00:00:00Z"}`,
	} {
		t.Run(terms, func(t *testing.T) {
			server := catalogStub(t, http.StatusOK, `{"products":[{"id":"arena","plans":[{"slug":"all-access","priceCents":999,"interval":"month","public":true,`+terms+`}]}]}`)
			source := NewPlanSource(server.URL, "arena", server.Client())
			if err := source.Refresh(t.Context()); err == nil {
				t.Fatal("invalid commercial terms must not produce a quote")
			}
			if _, ok := source.Get(); ok {
				t.Fatal("invalid terms remained available")
			}
		})
	}
}
