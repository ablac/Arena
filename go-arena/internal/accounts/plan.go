package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

/*
 * What Accounts charges for the Arena subscription, so the Shop can quote it.
 *
 * Arena sells nothing and holds no price. Quoting one from its own source
 * would put a second number in the world that can disagree with the card
 * charge, which is exactly what the subscription rewrite removed. So the Shop
 * says what Accounts says, read from the same public catalog a customer sees,
 * and marks the price unavailable when it does not know.
 *
 * The read is deliberately off the request path: the catalog endpoint answers
 * from a value refreshed in the background, so a slow or unreachable Accounts
 * costs the Shop nothing and an unknown price is simply a Shop that quotes no
 * figure — never a Shop that fails to load.
 */

// PlanMaxAge bounds a quote even if the background refresher stops running.
const PlanMaxAge = 45 * time.Second

// Sale is Accounts' offer. The discount applies to the subscription, not each
// seat. Redemption dates and discount duration are separate commercial terms.
type Sale struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	PercentOff     *float64   `json:"percentOff"`
	AmountOffCents *int       `json:"amountOffCents"`
	Duration       string     `json:"duration"`
	DurationMonths *int       `json:"durationMonths"`
	StartsAt       *time.Time `json:"startsAt"`
	EndsAt         *time.Time `json:"endsAt"`
}

// Plan is one product's public subscription plan, as Accounts sells it.
type Plan struct {
	Slug          string
	PriceCents    int
	Currency      string
	Interval      string
	PriceRevision int
	SeatsIncluded int
	Sale          *Sale
	ValidUntil    time.Time
}

// PlanSource remembers one product's plan from the Accounts catalog.
type PlanSource struct {
	catalogURL string
	productID  string
	http       *http.Client

	refreshMu sync.Mutex
	mu        sync.RWMutex
	plan      Plan
	known     bool
	now       func() time.Time
}

// NewPlanSource reads productID's plan from the catalog the issuer publishes.
// A blank issuer or product yields nil, which Get reports as "no price".
func NewPlanSource(issuer, productID string, httpClient *http.Client) *PlanSource {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	productID = strings.TrimSpace(productID)
	if issuer == "" || productID == "" {
		return nil
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &PlanSource{
		catalogURL: issuer + "/api/v1/catalog",
		productID:  productID,
		http:       httpClient,
		now:        time.Now,
	}
}

// Get returns a current quote without blocking. A failed refresh, age limit or
// sale boundary withdraws it; an old offer must not guide a purchase decision.
func (s *PlanSource) Get() (Plan, bool) {
	if s == nil {
		return Plan{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.known || !s.now().Before(s.plan.ValidUntil) {
		return Plan{}, false
	}
	return s.plan, true
}

// NextRefresh wakes the background reader at sale boundaries as well as at
// its capped interval. A price remains unavailable until that read succeeds.
func (s *PlanSource) NextRefresh(maximum time.Duration) time.Duration {
	if s == nil {
		return maximum
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.known {
		remaining := s.plan.ValidUntil.Sub(s.now())
		if remaining <= 0 {
			return time.Second
		}
		if remaining < maximum {
			return remaining
		}
	}
	return maximum
}

// Refresh reads the public catalog once. Errors immediately withdraw the
// previous quote; cosmetic access and existing subscriptions are independent.
func (s *PlanSource) Refresh(ctx context.Context) (refreshErr error) {
	if s == nil {
		return nil
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	defer func() {
		if refreshErr != nil {
			s.mu.Lock()
			s.known = false
			s.plan = Plan{}
			s.mu.Unlock()
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.catalogURL, nil)
	if err != nil {
		return fmt.Errorf("accounts catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache, no-store")
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("accounts catalog fetch: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("accounts catalog: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("accounts catalog read: %w", err)
	}

	var doc struct {
		Products []struct {
			ID    string `json:"id"`
			Slug  string `json:"slug"`
			Plans []struct {
				Slug          string `json:"slug"`
				PriceCents    int    `json:"priceCents"`
				Currency      string `json:"currency"`
				Interval      string `json:"interval"`
				Public        bool   `json:"public"`
				PriceRevision int    `json:"priceRevision"`
				SeatsIncluded *int   `json:"seatsIncluded"`
				Sale          *Sale  `json:"sale"`
			} `json:"plans"`
		} `json:"products"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("accounts catalog decode: %w", err)
	}

	for _, product := range doc.Products {
		if product.ID != s.productID && product.Slug != s.productID {
			continue
		}
		/*
		 * The cheapest public paid plan is what a Shop quotes: a free tier is
		 * not what "subscribe" costs, and a plan that is not public is not on
		 * offer to the person reading. Arena has exactly one today; choosing
		 * deliberately means a second one added in Accounts does not silently
		 * change the figure to whichever happens to be listed first.
		 */
		var best Plan
		found := false
		for _, plan := range product.Plans {
			if !plan.Public || plan.PriceCents <= 0 {
				continue
			}
			if found && plan.PriceCents >= best.PriceCents {
				continue
			}
			currency := strings.ToUpper(strings.TrimSpace(plan.Currency))
			if currency == "" {
				// Accounts' catalog contract uses USD and historically omitted it.
				currency = "USD"
			}
			seats := 1
			if plan.SeatsIncluded != nil {
				seats = *plan.SeatsIncluded
			}
			interval := strings.TrimSpace(plan.Interval)
			if currency != "USD" || seats <= 0 || plan.PriceRevision < 0 || (interval != "month" && interval != "year") {
				return fmt.Errorf("accounts catalog: invalid commercial terms for %q", plan.Slug)
			}
			if err := validateSale(plan.Sale); err != nil {
				return fmt.Errorf("accounts catalog: %w", err)
			}
			now := s.now()
			sale := plan.Sale
			if sale != nil && sale.EndsAt != nil && !now.Before(*sale.EndsAt) {
				sale = nil
			}
			validUntil := now.Add(PlanMaxAge)
			if sale != nil {
				for _, boundary := range []*time.Time{sale.StartsAt, sale.EndsAt} {
					if boundary != nil && boundary.After(now) && boundary.Before(validUntil) {
						validUntil = *boundary
					}
				}
			}
			best = Plan{
				Slug:          plan.Slug,
				PriceCents:    plan.PriceCents,
				Currency:      currency,
				Interval:      interval,
				PriceRevision: plan.PriceRevision,
				SeatsIncluded: seats,
				Sale:          sale,
				ValidUntil:    validUntil,
			}
			found = true
		}
		if !found {
			return fmt.Errorf("accounts catalog: %s publishes no public paid plan", s.productID)
		}
		s.mu.Lock()
		s.plan = best
		s.known = true
		s.mu.Unlock()
		return nil
	}
	return fmt.Errorf("accounts catalog: no product %q", s.productID)
}

func validateSale(sale *Sale) error {
	if sale == nil {
		return nil
	}
	if strings.TrimSpace(sale.ID) == "" || strings.TrimSpace(sale.Name) == "" || (sale.PercentOff == nil) == (sale.AmountOffCents == nil) {
		return fmt.Errorf("incomplete sale")
	}
	if sale.PercentOff != nil && (*sale.PercentOff <= 0 || *sale.PercentOff > 100) {
		return fmt.Errorf("invalid sale percentage")
	}
	if sale.AmountOffCents != nil && *sale.AmountOffCents <= 0 {
		return fmt.Errorf("invalid sale amount")
	}
	switch sale.Duration {
	case "once", "forever":
		if sale.DurationMonths != nil {
			return fmt.Errorf("unexpected sale duration months")
		}
	case "repeating":
		if sale.DurationMonths == nil || *sale.DurationMonths <= 0 {
			return fmt.Errorf("missing sale duration months")
		}
	default:
		return fmt.Errorf("unknown sale duration")
	}
	if sale.StartsAt != nil && sale.EndsAt != nil && !sale.StartsAt.Before(*sale.EndsAt) {
		return fmt.Errorf("invalid sale redemption dates")
	}
	return nil
}
