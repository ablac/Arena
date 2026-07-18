package platform

import (
	"context"
	"time"

	"arena-server/internal/db"
)

// PostgresAuthority is the same-database W1b strangler. It is the only shared
// authority adapter; it forwards to the existing, transactionally proven Arena
// records and does not create a second writable copy.
type PostgresAuthority struct{}

var _ Authority = PostgresAuthority{}

func NewPostgresAuthority() PostgresAuthority { return PostgresAuthority{} }

func (PostgresAuthority) UpsertVerifiedIdentity(ctx context.Context, email, issuer, subject, displayName string) (*db.CustomerAccount, error) {
	return db.UpsertVerifiedCustomerAccount(ctx, email, issuer, subject, displayName)
}

func (PostgresAuthority) PublicCatalog(ctx context.Context) (*db.CosmeticCatalog, error) {
	if db.Pool == nil {
		catalog := db.DefaultCosmeticCatalogData()
		return &catalog, nil
	}
	return db.GetPublicCosmeticCatalog(ctx)
}

func (PostgresAuthority) AdminCatalog(ctx context.Context) (*db.CosmeticCatalog, error) {
	return db.GetAdminCosmeticCatalog(ctx)
}

func (PostgresAuthority) UpsertCategory(ctx context.Context, category db.CosmeticCategory, actor string) (*db.CosmeticCategory, error) {
	return db.UpsertCosmeticCategory(ctx, category, actor)
}

func (PostgresAuthority) DeleteCategory(ctx context.Context, categoryID, actor string) (bool, error) {
	return db.DeleteCosmeticCategory(ctx, categoryID, actor)
}

func (PostgresAuthority) UpsertItem(ctx context.Context, item db.CosmeticItem, actor string) (*db.CosmeticItem, error) {
	return db.UpsertCosmeticCatalogItem(ctx, item, actor)
}

func (PostgresAuthority) DeleteItem(ctx context.Context, itemID, actor string) (bool, error) {
	return db.DeleteCosmeticCatalogItem(ctx, itemID, actor)
}

func (PostgresAuthority) UpsertPack(ctx context.Context, pack db.CosmeticPack, actor string) (*db.CosmeticPack, error) {
	return db.UpsertCosmeticPack(ctx, pack, actor)
}

func (PostgresAuthority) DeletePack(ctx context.Context, packID, actor string) (bool, error) {
	return db.DeleteCosmeticPack(ctx, packID, actor)
}

func (PostgresAuthority) ListAudit(ctx context.Context, limit int) ([]db.CosmeticCatalogAudit, error) {
	return db.ListCosmeticCatalogAudit(ctx, limit)
}

func (PostgresAuthority) AccountInventory(ctx context.Context, accountID string) (*db.CustomerCosmeticsInventory, error) {
	return db.GetCustomerCosmeticsInventory(ctx, accountID)
}

func (PostgresAuthority) LinkAgent(ctx context.Context, accountID, agentID string) (*db.AccountBot, error) {
	return db.LinkBotToCustomerAccount(ctx, accountID, agentID)
}

func (PostgresAuthority) UnlinkAgent(ctx context.Context, accountID, agentID string) (bool, error) {
	return db.UnlinkBotFromCustomerAccount(ctx, accountID, agentID)
}

func (PostgresAuthority) AssignLicense(ctx context.Context, accountID, licenseID string, agentID *string) (*db.CosmeticAssignmentChange, error) {
	return db.AssignCosmeticLicense(ctx, accountID, licenseID, agentID)
}

func (PostgresAuthority) GrantLicense(ctx context.Context, email, cosmeticID, source, externalReference string) (*db.CosmeticLicense, bool, error) {
	return db.GrantCosmeticLicense(ctx, email, cosmeticID, source, externalReference)
}

func (PostgresAuthority) RevokeLicense(ctx context.Context, licenseID string) (*db.CosmeticAssignmentChange, bool, error) {
	return db.RevokeCosmeticLicense(ctx, licenseID)
}

func (PostgresAuthority) AdminAccess(ctx context.Context, email string) (*db.CosmeticAdminAccess, error) {
	return db.GetCosmeticAdminAccessByEmail(ctx, email)
}

func (PostgresAuthority) CreateAdminMembership(ctx context.Context, email string, expiresAt time.Time, note, actor string) (*db.CosmeticAdminMembership, int, error) {
	return db.CreateCosmeticAdminMembership(ctx, email, expiresAt, note, actor)
}

func (PostgresAuthority) RevokeAdminMembership(ctx context.Context, membershipID, actor, reason string) (*db.CosmeticAdminMembership, []string, bool, error) {
	return db.RevokeCosmeticAdminMembership(ctx, membershipID, actor, reason)
}

func (PostgresAuthority) ExpireAdminMembershipsForEmail(ctx context.Context, email string, now time.Time) (int, []string, error) {
	return db.ExpireCustomerCosmeticAdminMemberships(ctx, email, now)
}
