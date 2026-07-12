package config

import (
	"os"
	"strings"
	"testing"
)

func validCosmeticsCheckoutConfig() Config {
	return Config{
		DBOptional:               false,
		CustomerOIDCEnabled:      true,
		CustomerOIDCIssuer:       "https://identity.example",
		CustomerOIDCClientID:     "arena-customers",
		CustomerOIDCClientSecret: "client-secret",
		CustomerOIDCRedirectURI:  "https://arena.example/api/v1/account/callback",
		CustomerOIDCSessionTTL:   24,
		CosmeticsCheckoutEnabled: true,
		StripeSecretKey:          "sk_test_checkout",
		StripeWebhookSecrets:     "whsec_current,whsec_previous",
		StripeSuccessURL:         "https://arena.example/dashboard?checkout=success",
		StripeCancelURL:          "https://arena.example/shop?checkout=cancelled",
		StripePortalReturnURL:    "https://arena.example/dashboard?tab=cosmetics",
		CosmeticsCheckoutRPM:     10,
		CosmeticsAccountReadRPM:  60,
	}
}

func TestValidateCosmeticsCheckoutConfigFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "customer OIDC disabled", mutate: func(cfg *Config) { cfg.CustomerOIDCEnabled = false }, want: "customer OIDC"},
		{name: "customer OIDC issuer missing", mutate: func(cfg *Config) { cfg.CustomerOIDCIssuer = "" }, want: "customer OIDC"},
		{name: "customer OIDC client ID missing", mutate: func(cfg *Config) { cfg.CustomerOIDCClientID = "" }, want: "customer OIDC"},
		{name: "customer OIDC client secret missing", mutate: func(cfg *Config) { cfg.CustomerOIDCClientSecret = "" }, want: "customer OIDC"},
		{name: "customer OIDC redirect missing", mutate: func(cfg *Config) { cfg.CustomerOIDCRedirectURI = "" }, want: "customer OIDC"},
		{name: "customer OIDC session disabled", mutate: func(cfg *Config) { cfg.CustomerOIDCSessionTTL = 0 }, want: "customer OIDC"},
		{name: "database optional", mutate: func(cfg *Config) { cfg.DBOptional = true }, want: "database"},
		{name: "Stripe key missing", mutate: func(cfg *Config) { cfg.StripeSecretKey = " " }, want: "ARENA_STRIPE_SECRET_KEY"},
		{name: "webhook secrets missing", mutate: func(cfg *Config) { cfg.StripeWebhookSecrets = ", " }, want: "ARENA_STRIPE_WEBHOOK_SECRETS"},
		{name: "relative success URL", mutate: func(cfg *Config) { cfg.StripeSuccessURL = "/dashboard" }, want: "ARENA_STRIPE_SUCCESS_URL"},
		{name: "insecure public success URL", mutate: func(cfg *Config) { cfg.StripeSuccessURL = "http://arena.example/dashboard" }, want: "ARENA_STRIPE_SUCCESS_URL"},
		{name: "relative cancel URL", mutate: func(cfg *Config) { cfg.StripeCancelURL = "/shop" }, want: "ARENA_STRIPE_CANCEL_URL"},
		{name: "unsupported cancel URL scheme", mutate: func(cfg *Config) { cfg.StripeCancelURL = "ftp://arena.example/shop" }, want: "ARENA_STRIPE_CANCEL_URL"},
		{name: "relative portal return URL", mutate: func(cfg *Config) { cfg.StripePortalReturnURL = "/dashboard" }, want: "ARENA_STRIPE_PORTAL_RETURN_URL"},
		{name: "checkout rate disabled", mutate: func(cfg *Config) { cfg.CosmeticsCheckoutRPM = 0 }, want: "ARENA_COSMETICS_CHECKOUT_RPM"},
		{name: "account cosmetics read rate disabled", mutate: func(cfg *Config) { cfg.CosmeticsAccountReadRPM = 0 }, want: "ARENA_COSMETICS_ACCOUNT_READ_RPM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validCosmeticsCheckoutConfig()
			tt.mutate(&cfg)
			err := ValidateCosmeticsCheckoutConfig(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateCosmeticsCheckoutConfig() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateCosmeticsCheckoutConfigRequiresStripeAPIKeyDuringWebhookSalesPause(t *testing.T) {
	cfg := Config{
		CosmeticsCheckoutEnabled: false,
		StripeWebhookSecrets:     "whsec_existing_subscriptions",
		CosmeticsAccountReadRPM:  60,
		StripePortalReturnURL:    "https://arena.example/dashboard?tab=cosmetics",
	}
	if err := ValidateCosmeticsCheckoutConfig(cfg); err == nil || !strings.Contains(err.Error(), "ARENA_STRIPE_SECRET_KEY") {
		t.Fatalf("sales-pause validation error = %v, want retained Stripe API key requirement", err)
	}
	cfg.StripeSecretKey = "sk_live_retained_for_subscription_reconciliation"
	portalReturnURL := cfg.StripePortalReturnURL
	cfg.StripePortalReturnURL = ""
	if err := ValidateCosmeticsCheckoutConfig(cfg); err == nil || !strings.Contains(err.Error(), "ARENA_STRIPE_PORTAL_RETURN_URL") {
		t.Fatalf("sales-pause portal validation error = %v", err)
	}
	cfg.StripePortalReturnURL = portalReturnURL
	if err := ValidateCosmeticsCheckoutConfig(cfg); err != nil {
		t.Fatalf("sales pause with retained Stripe API key rejected: %v", err)
	}
}

func TestValidateCosmeticsCheckoutConfigRequiresAccountReadRateDuringSalesPause(t *testing.T) {
	cfg := Config{CosmeticsAccountReadRPM: 0}
	if err := ValidateCosmeticsCheckoutConfig(cfg); err == nil || !strings.Contains(err.Error(), "ARENA_COSMETICS_ACCOUNT_READ_RPM") {
		t.Fatalf("disabled checkout read-rate validation error = %v", err)
	}
}

func TestValidateCosmeticsCheckoutConfigAllowsDisabledAndLoopbackDevelopment(t *testing.T) {
	if err := ValidateCosmeticsCheckoutConfig(Config{CosmeticsAccountReadRPM: 60}); err != nil {
		t.Fatalf("disabled checkout should not require payment config: %v", err)
	}

	cfg := validCosmeticsCheckoutConfig()
	cfg.StripeSuccessURL = "http://localhost:8000/dashboard"
	cfg.StripeCancelURL = "http://127.0.0.1:8000/shop"
	cfg.StripePortalReturnURL = "http://localhost:8000/dashboard"
	if err := ValidateCosmeticsCheckoutConfig(cfg); err != nil {
		t.Fatalf("loopback HTTP checkout URLs should be allowed for development: %v", err)
	}

	cfg.StripeSuccessURL = "http://[::1]:8000/dashboard"
	if err := ValidateCosmeticsCheckoutConfig(cfg); err != nil {
		t.Fatalf("IPv6 loopback HTTP checkout URL should be allowed for development: %v", err)
	}
}

func validCustomerEmailAuthConfig() Config {
	return Config{
		DBOptional:                       false,
		CustomerOIDCSessionTTL:           24,
		CustomerEmailAuthEnabled:         true,
		CustomerEmailSignInURL:           "https://arena.example/dashboard/",
		CustomerEmailTokenTTLMinutes:     15,
		CustomerEmailSendCooldownSeconds: 60,
		CustomerEmailSendRPM:             5,
		SMTPHost:                         "stalwart",
		SMTPPort:                         465,
		SMTPTLSMode:                      "implicit",
		SMTPTLSServerName:                "mail.angel-serv.com",
		SMTPUsername:                     "noreply@angel-serv.com",
		SMTPPassword:                     "mailbox-secret",
		SMTPFrom:                         "Arena <noreply@angel-serv.com>",
	}
}

func TestValidateCustomerEmailAuthConfigFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "database optional", mutate: func(cfg *Config) { cfg.DBOptional = true }, want: "database"},
		{name: "sign-in URL missing", mutate: func(cfg *Config) { cfg.CustomerEmailSignInURL = "" }, want: "ARENA_CUSTOMER_EMAIL_SIGN_IN_URL"},
		{name: "public HTTP URL", mutate: func(cfg *Config) { cfg.CustomerEmailSignInURL = "http://arena.example/dashboard/" }, want: "ARENA_CUSTOMER_EMAIL_SIGN_IN_URL"},
		{name: "token TTL disabled", mutate: func(cfg *Config) { cfg.CustomerEmailTokenTTLMinutes = 0 }, want: "ARENA_CUSTOMER_EMAIL_TOKEN_TTL_MINUTES"},
		{name: "cooldown disabled", mutate: func(cfg *Config) { cfg.CustomerEmailSendCooldownSeconds = 0 }, want: "ARENA_CUSTOMER_EMAIL_SEND_COOLDOWN_SECONDS"},
		{name: "rate disabled", mutate: func(cfg *Config) { cfg.CustomerEmailSendRPM = 0 }, want: "ARENA_CUSTOMER_EMAIL_SEND_RPM"},
		{name: "SMTP host missing", mutate: func(cfg *Config) { cfg.SMTPHost = "" }, want: "ARENA_SMTP_HOST"},
		{name: "SMTP port invalid", mutate: func(cfg *Config) { cfg.SMTPPort = 0 }, want: "ARENA_SMTP_PORT"},
		{name: "SMTP transport insecure", mutate: func(cfg *Config) { cfg.SMTPTLSMode = "none" }, want: "ARENA_SMTP_TLS_MODE"},
		{name: "TLS name missing", mutate: func(cfg *Config) { cfg.SMTPTLSServerName = "" }, want: "ARENA_SMTP_TLS_SERVER_NAME"},
		{name: "username missing", mutate: func(cfg *Config) { cfg.SMTPUsername = "" }, want: "ARENA_SMTP_USERNAME"},
		{name: "password missing", mutate: func(cfg *Config) { cfg.SMTPPassword = "" }, want: "ARENA_SMTP_PASSWORD"},
		{name: "from invalid", mutate: func(cfg *Config) { cfg.SMTPFrom = "not-an-address" }, want: "ARENA_SMTP_FROM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validCustomerEmailAuthConfig()
			tt.mutate(&cfg)
			err := ValidateCustomerEmailAuthConfig(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateCustomerEmailAuthConfig() error = %v, want %q", err, tt.want)
			}
		})
	}
	if err := ValidateCustomerEmailAuthConfig(Config{}); err != nil {
		t.Fatalf("disabled customer email auth should not require SMTP: %v", err)
	}
	loopback := validCustomerEmailAuthConfig()
	loopback.CustomerEmailSignInURL = "http://127.0.0.1:8000/dashboard/"
	if err := ValidateCustomerEmailAuthConfig(loopback); err != nil {
		t.Fatalf("loopback development URL: %v", err)
	}
}

func TestCosmeticsCheckoutAcceptsCompleteEmailAuthInsteadOfOIDC(t *testing.T) {
	cfg := validCosmeticsCheckoutConfig()
	cfg.CustomerOIDCEnabled = false
	email := validCustomerEmailAuthConfig()
	cfg.CustomerEmailAuthEnabled = true
	cfg.CustomerEmailSignInURL = email.CustomerEmailSignInURL
	cfg.CustomerEmailTokenTTLMinutes = email.CustomerEmailTokenTTLMinutes
	cfg.CustomerEmailSendCooldownSeconds = email.CustomerEmailSendCooldownSeconds
	cfg.CustomerEmailSendRPM = email.CustomerEmailSendRPM
	cfg.SMTPHost = email.SMTPHost
	cfg.SMTPPort = email.SMTPPort
	cfg.SMTPTLSMode = email.SMTPTLSMode
	cfg.SMTPTLSServerName = email.SMTPTLSServerName
	cfg.SMTPUsername = email.SMTPUsername
	cfg.SMTPPassword = email.SMTPPassword
	cfg.SMTPFrom = email.SMTPFrom
	if err := ValidateCosmeticsCheckoutConfig(cfg); err != nil {
		t.Fatalf("complete verified-email auth should satisfy checkout: %v", err)
	}
}

func TestLoadReadsNativeCustomerEmailAuth(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	t.Setenv("ARENA_COSMETICS_CHECKOUT_ENABLED", "false")
	t.Setenv("ARENA_DB_OPTIONAL", "false")
	t.Setenv("ARENA_CUSTOMER_EMAIL_AUTH_ENABLED", "true")
	t.Setenv("ARENA_CUSTOMER_EMAIL_SIGN_IN_URL", "https://arena.example/dashboard/")
	t.Setenv("ARENA_CUSTOMER_EMAIL_TOKEN_TTL_MINUTES", "15")
	t.Setenv("ARENA_CUSTOMER_EMAIL_SEND_COOLDOWN_SECONDS", "60")
	t.Setenv("ARENA_CUSTOMER_EMAIL_SEND_RPM", "5")
	t.Setenv("ARENA_CUSTOMER_OIDC_SESSION_TTL_HOURS", "24")
	t.Setenv("ARENA_SMTP_HOST", "100.71.171.28")
	t.Setenv("ARENA_SMTP_PORT", "465")
	t.Setenv("ARENA_SMTP_TLS_MODE", "implicit")
	t.Setenv("ARENA_SMTP_TLS_SERVER_NAME", "mail.angel-serv.com")
	t.Setenv("ARENA_SMTP_USERNAME", "noreply@angel-serv.com")
	t.Setenv("ARENA_SMTP_PASSWORD", "send-only-app-password")
	t.Setenv("ARENA_SMTP_FROM", "Arena <noreply@angel-serv.com>")
	C = Config{}
	Load()
	if !C.CustomerEmailAuthEnabled || C.CustomerEmailTokenTTLMinutes != 15 ||
		C.CustomerEmailSendCooldownSeconds != 60 || C.CustomerEmailSendRPM != 5 ||
		C.SMTPHost != "100.71.171.28" || C.SMTPPort != 465 || C.SMTPTLSMode != "implicit" ||
		C.SMTPTLSServerName != "mail.angel-serv.com" || C.SMTPUsername != "noreply@angel-serv.com" {
		t.Fatalf("native email auth config was not loaded: %+v", C)
	}
}

func TestLoadReadsCosmeticsCheckoutDefaultsAndSecretRotation(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	for _, key := range []string{
		"ARENA_COSMETICS_CHECKOUT_ENABLED",
		"ARENA_STRIPE_SECRET_KEY",
		"ARENA_STRIPE_WEBHOOK_SECRETS",
		"ARENA_STRIPE_SUCCESS_URL",
		"ARENA_STRIPE_CANCEL_URL",
		"ARENA_STRIPE_PORTAL_RETURN_URL",
		"ARENA_STRIPE_AUTOMATIC_TAX",
		"ARENA_COSMETICS_CHECKOUT_RPM",
	} {
		value, existed := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	C = Config{}
	Load()
	if C.CosmeticsCheckoutEnabled || C.StripeAutomaticTax {
		t.Fatal("cosmetics checkout and automatic tax must default disabled")
	}
	if C.CosmeticsCheckoutRPM != 10 {
		t.Fatalf("CosmeticsCheckoutRPM = %d, want 10", C.CosmeticsCheckoutRPM)
	}
	if C.CosmeticsAccountReadRPM != 60 {
		t.Fatalf("CosmeticsAccountReadRPM = %d, want 60", C.CosmeticsAccountReadRPM)
	}

	t.Setenv("ARENA_COSMETICS_CHECKOUT_ENABLED", "true")
	t.Setenv("ARENA_CUSTOMER_OIDC_ENABLED", "true")
	t.Setenv("ARENA_CUSTOMER_OIDC_ISSUER", "https://identity.example")
	t.Setenv("ARENA_CUSTOMER_OIDC_CLIENT_ID", "arena-customers")
	t.Setenv("ARENA_CUSTOMER_OIDC_CLIENT_SECRET", "client-secret")
	t.Setenv("ARENA_CUSTOMER_OIDC_REDIRECT_URI", "https://arena.example/api/v1/account/callback")
	t.Setenv("ARENA_CUSTOMER_OIDC_SESSION_TTL_HOURS", "24")
	t.Setenv("ARENA_DB_OPTIONAL", "false")
	t.Setenv("ARENA_STRIPE_SECRET_KEY", "sk_test_checkout")
	t.Setenv("ARENA_STRIPE_WEBHOOK_SECRETS", "whsec_current, whsec_previous")
	t.Setenv("ARENA_STRIPE_SUCCESS_URL", "https://arena.example/dashboard")
	t.Setenv("ARENA_STRIPE_CANCEL_URL", "https://arena.example/shop")
	t.Setenv("ARENA_STRIPE_PORTAL_RETURN_URL", "https://arena.example/dashboard")
	C = Config{}
	Load()
	secrets := ParseStripeWebhookSecrets(C.StripeWebhookSecrets)
	if len(secrets) != 2 || secrets[0] != "whsec_current" || secrets[1] != "whsec_previous" {
		t.Fatalf("ParseStripeWebhookSecrets() = %#v, want normalized rotation list", secrets)
	}
}

func TestLoadInvokesCosmeticsCheckoutValidation(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	t.Setenv("ARENA_COSMETICS_CHECKOUT_ENABLED", "true")
	t.Setenv("ARENA_CUSTOMER_OIDC_ENABLED", "false")
	C = Config{}

	defer func() {
		if recover() == nil {
			t.Fatal("Load() did not fail closed for incomplete checkout configuration")
		}
	}()
	Load()
}

func TestLoadReadsManagedMigrationRoles(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	t.Setenv("ARENA_DB_MIGRATIONS_MANAGED", "true")
	t.Setenv("ARENA_RUNTIME_DB_USER", "arena_app")

	Load()
	if !C.DBMigrationsManaged {
		t.Fatal("ARENA_DB_MIGRATIONS_MANAGED=true was not loaded")
	}
	if C.DBRuntimeUser != "arena_app" {
		t.Fatalf("ARENA_RUNTIME_DB_USER = %q, want arena_app", C.DBRuntimeUser)
	}
	if ShouldAutoMigrateDatabase() {
		t.Fatal("managed runtime must not attempt schema DDL")
	}

	C.DBMigrationsManaged = false
	if !ShouldAutoMigrateDatabase() {
		t.Fatal("single-role local runtime should retain automatic migrations")
	}
}

func TestLoadReadsCustomerAPIKeyAbuseLimits(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	t.Setenv("ARENA_CUSTOMER_API_KEY_MUTATION_RPM", "31")
	t.Setenv("ARENA_CUSTOMER_API_KEY_CREATE_PER_HOUR", "11")
	t.Setenv("ARENA_CUSTOMER_API_KEY_REVOKE_PER_HOUR", "21")
	t.Setenv("ARENA_CUSTOMER_BOT_LINK_PER_HOUR", "12")
	C = Config{}

	Load()

	if C.CustomerAPIKeyMutationRPM != 31 || C.CustomerAPIKeyCreatePerHour != 11 ||
		C.CustomerAPIKeyRevokePerHour != 21 || C.CustomerBotLinkPerHour != 12 {
		t.Fatalf("customer API-key abuse limits were not loaded: %+v", C)
	}
}

func TestResolveShoveSettingsUsesWholePositiveGridTiles(t *testing.T) {
	tests := []struct {
		name                     string
		rangeIn, knockbackIn     float64
		rangeWant, knockbackWant float64
	}{
		{name: "defaults remain exact", rangeIn: 1, knockbackIn: 2, rangeWant: 1, knockbackWant: 2},
		{name: "fractional overrides round once", rangeIn: 1.6, knockbackIn: 3.4, rangeWant: 2, knockbackWant: 3},
		{name: "nonpositive values use defaults", rangeIn: 0, knockbackIn: -2, rangeWant: DefaultShoveRangeTiles, knockbackWant: DefaultShoveKnockbackTiles},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rangeGot, knockbackGot := resolveShoveSettings(tt.rangeIn, tt.knockbackIn)
			if rangeGot != tt.rangeWant || knockbackGot != tt.knockbackWant {
				t.Fatalf("resolveShoveSettings(%v, %v) = (%v, %v), want (%v, %v)", tt.rangeIn, tt.knockbackIn, rangeGot, knockbackGot, tt.rangeWant, tt.knockbackWant)
			}
		})
	}
}

func TestResolveEloSettings(t *testing.T) {
	tests := []struct {
		name                           string
		min, max, starting             int
		wantMin, wantMax, wantStarting int
	}{
		{name: "valid custom values", min: 800, max: 1600, starting: 1200, wantMin: 800, wantMax: 1600, wantStarting: 1200},
		{name: "inverted bounds use one default pair", min: 5000, max: 2000, starting: 1500, wantMin: DefaultEloMin, wantMax: DefaultEloMax, wantStarting: 1500},
		{name: "nonpositive bounds use one default pair", min: 0, max: 0, starting: 1000, wantMin: DefaultEloMin, wantMax: DefaultEloMax, wantStarting: 1000},
		{name: "high starting rating clamps", min: 800, max: 1200, starting: 5000, wantMin: 800, wantMax: 1200, wantStarting: 1200},
		{name: "low starting rating clamps", min: 800, max: 1200, starting: 200, wantMin: 800, wantMax: 1200, wantStarting: 800},
		{name: "missing starting rating uses bounded default", min: 1500, max: 2000, starting: 0, wantMin: 1500, wantMax: 2000, wantStarting: 1500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minElo, maxElo, startingElo := resolveEloSettings(tt.min, tt.max, tt.starting)
			if minElo != tt.wantMin || maxElo != tt.wantMax || startingElo != tt.wantStarting {
				t.Fatalf("resolveEloSettings(%d, %d, %d) = (%d, %d, %d), want (%d, %d, %d)",
					tt.min, tt.max, tt.starting, minElo, maxElo, startingElo,
					tt.wantMin, tt.wantMax, tt.wantStarting)
			}
		})
	}
}

func TestEloHelpersUseSameDefensiveBounds(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	C.EloMin = 5000
	C.EloMax = 2000
	C.EloStarting = 9000

	minElo, maxElo := EloBounds()
	if minElo != DefaultEloMin || maxElo != DefaultEloMax {
		t.Fatalf("EloBounds() = %d..%d, want %d..%d", minElo, maxElo, DefaultEloMin, DefaultEloMax)
	}
	if got := StartingElo(); got != DefaultEloMax {
		t.Fatalf("StartingElo() = %d, want %d", got, DefaultEloMax)
	}
	if got := ClampElo(-1); got != DefaultEloMin {
		t.Fatalf("ClampElo(-1) = %d, want %d", got, DefaultEloMin)
	}
	if got := ClampElo(99999); got != DefaultEloMax {
		t.Fatalf("ClampElo(99999) = %d, want %d", got, DefaultEloMax)
	}
}

func TestResolveWeaponAutoBalanceSettings(t *testing.T) {
	tests := []struct {
		name                             string
		minDamage, maxDamage             float64
		minCooldown, maxCooldown         float64
		maxEvidenceRounds                int
		wantMinDamage, wantMaxDamage     float64
		wantMinCooldown, wantMaxCooldown float64
		wantMaxEvidenceRounds            int
	}{
		{
			name: "valid widened rails", minDamage: 0.65, maxDamage: 1.50,
			minCooldown: 0.70, maxCooldown: 1.45, maxEvidenceRounds: 72,
			wantMinDamage: 0.65, wantMaxDamage: 1.50,
			wantMinCooldown: 0.70, wantMaxCooldown: 1.45, wantMaxEvidenceRounds: 72,
		},
		{
			name: "inverted damage rail falls back", minDamage: 1.20, maxDamage: 0.80,
			minCooldown: 0.75, maxCooldown: 1.35, maxEvidenceRounds: 48,
			wantMinDamage: DefaultWeaponAutoBalanceMinDamageScale, wantMaxDamage: DefaultWeaponAutoBalanceMaxDamageScale,
			wantMinCooldown: 0.75, wantMaxCooldown: 1.35, wantMaxEvidenceRounds: 48,
		},
		{
			name: "rails must contain neutral", minDamage: 1.05, maxDamage: 1.50,
			minCooldown: 0.20, maxCooldown: 0.90, maxEvidenceRounds: 1,
			wantMinDamage: DefaultWeaponAutoBalanceMinDamageScale, wantMaxDamage: DefaultWeaponAutoBalanceMaxDamageScale,
			wantMinCooldown: DefaultWeaponAutoBalanceMinCooldownScale, wantMaxCooldown: DefaultWeaponAutoBalanceMaxCooldownScale,
			wantMaxEvidenceRounds: DefaultWeaponAutoBalanceMaxEvidenceRounds,
		},
		{
			name: "absolute safety rails reject extreme values", minDamage: 0.01, maxDamage: 9,
			minCooldown: 0.01, maxCooldown: 9, maxEvidenceRounds: 9999,
			wantMinDamage: DefaultWeaponAutoBalanceMinDamageScale, wantMaxDamage: DefaultWeaponAutoBalanceMaxDamageScale,
			wantMinCooldown: DefaultWeaponAutoBalanceMinCooldownScale, wantMaxCooldown: DefaultWeaponAutoBalanceMaxCooldownScale,
			wantMaxEvidenceRounds: DefaultWeaponAutoBalanceMaxEvidenceRounds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minDamage, maxDamage, minCooldown, maxCooldown, maxEvidenceRounds := resolveWeaponAutoBalanceSettings(
				tt.minDamage, tt.maxDamage, tt.minCooldown, tt.maxCooldown, tt.maxEvidenceRounds,
			)
			if minDamage != tt.wantMinDamage || maxDamage != tt.wantMaxDamage ||
				minCooldown != tt.wantMinCooldown || maxCooldown != tt.wantMaxCooldown ||
				maxEvidenceRounds != tt.wantMaxEvidenceRounds {
				t.Fatalf("resolved balance settings = %.2f..%.2f / %.2f..%.2f / %d, want %.2f..%.2f / %.2f..%.2f / %d",
					minDamage, maxDamage, minCooldown, maxCooldown, maxEvidenceRounds,
					tt.wantMinDamage, tt.wantMaxDamage, tt.wantMinCooldown, tt.wantMaxCooldown, tt.wantMaxEvidenceRounds)
			}
		})
	}
}

func TestWeaponAutoBalanceHelpersUseDefensiveDefaults(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	C.WeaponAutoBalanceMinDamageScale = 2
	C.WeaponAutoBalanceMaxDamageScale = 3
	C.WeaponAutoBalanceMinCooldownScale = -1
	C.WeaponAutoBalanceMaxCooldownScale = 4
	C.WeaponAutoBalanceMaxEvidenceRounds = 0

	minDamage, maxDamage := WeaponAutoBalanceDamageBounds()
	minCooldown, maxCooldown := WeaponAutoBalanceCooldownBounds()
	if minDamage != DefaultWeaponAutoBalanceMinDamageScale || maxDamage != DefaultWeaponAutoBalanceMaxDamageScale {
		t.Fatalf("damage bounds = %.2f..%.2f", minDamage, maxDamage)
	}
	if minCooldown != DefaultWeaponAutoBalanceMinCooldownScale || maxCooldown != DefaultWeaponAutoBalanceMaxCooldownScale {
		t.Fatalf("cooldown bounds = %.2f..%.2f", minCooldown, maxCooldown)
	}
	if got := WeaponAutoBalanceEvidenceLimit(6); got != DefaultWeaponAutoBalanceMaxEvidenceRounds {
		t.Fatalf("evidence limit = %d, want %d", got, DefaultWeaponAutoBalanceMaxEvidenceRounds)
	}
}

func TestWeaponAutoBalanceStepBounds(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })

	C.WeaponAutoBalanceMinStep = 0.004
	C.WeaponAutoBalanceStartStep = 0.04
	if minStep, startStep := WeaponAutoBalanceStepBounds(); minStep != 0.004 || startStep != 0.04 {
		t.Fatalf("valid step bounds = %.3f/%.3f, want 0.004/0.040", minStep, startStep)
	}

	C.WeaponAutoBalanceMinStep = -1
	C.WeaponAutoBalanceStartStep = 9
	if minStep, startStep := WeaponAutoBalanceStepBounds(); minStep != 0.005 || startStep != 0.05 {
		t.Fatalf("defensive step bounds = %.3f/%.3f, want 0.005/0.050", minStep, startStep)
	}
}
