// Package platform defines Arena's dependency on the shared identity and
// cosmetics authority. The first adapter remains in-process and uses Arena's
// existing PostgreSQL records; callers depend on this port so the records can
// move behind the versioned platform API without another ownership rewrite.
package platform

import (
	"context"
	"time"

	"arena-server/internal/db"
)

// IdentityAuthority owns verified customer identity binding. Customer
// sessions and credentials remain private to Arena.
type IdentityAuthority interface {
	UpsertVerifiedIdentity(context.Context, string, string, string, string) (*db.CustomerAccount, error)
}

// MetadataAuthority exposes the revisioned W1b.2 agent/profile contract. Its
// same-database implementation is ready for the later versioned platform HTTP
// adapter; W1b.2 deliberately does not expose a new public route.
type MetadataAuthority interface {
	AccountCapacity(context.Context, string) (*db.PlatformAccountCapacity, error)
	TransitionProfile(context.Context, db.PlatformProfileTransition) (*db.PlatformProfileTransitionResult, error)
	Changes(context.Context, int64, int) ([]db.PlatformChange, int64, error)
	AgentLinkHistory(context.Context, string, int64, int) ([]db.PlatformAgentLinkEvent, int64, error)
	LinkAgent(context.Context, db.PlatformAgentLinkCommand) (*db.PlatformAgentLinkResult, error)
}

// CosmeticsAuthority owns shared catalog, account-agent links, licenses, and
// their lifecycle. Bot loadout reads and equip writes are intentionally absent:
// those are Arena gameplay presentation state.
type CosmeticsAuthority interface {
	PublicCatalog(context.Context) (*db.CosmeticCatalog, error)
	AdminCatalog(context.Context) (*db.CosmeticCatalog, error)
	UpsertCategory(context.Context, db.CosmeticCategory, string) (*db.CosmeticCategory, error)
	DeleteCategory(context.Context, string, string) (bool, error)
	UpsertItem(context.Context, db.CosmeticItem, string) (*db.CosmeticItem, error)
	DeleteItem(context.Context, string, string) (bool, error)
	UpsertPack(context.Context, db.CosmeticPack, string) (*db.CosmeticPack, error)
	DeletePack(context.Context, string, string) (bool, error)
	ListAudit(context.Context, int) ([]db.CosmeticCatalogAudit, error)
	AccountInventory(context.Context, string) (*db.CustomerCosmeticsInventory, error)
	ClaimArenaAgent(context.Context, string, string) (*db.AccountBot, error)
	UnlinkAgent(context.Context, string, string) (bool, error)
	AssignLicense(context.Context, string, string, *string) (*db.CosmeticAssignmentChange, error)
	GrantLicense(context.Context, string, string, string, string) (*db.CosmeticLicense, bool, error)
	RevokeLicense(context.Context, string) (*db.CosmeticAssignmentChange, bool, error)
	AdminAccess(context.Context, string) (*db.CosmeticAdminAccess, error)
	CreateAdminMembership(context.Context, string, time.Time, string, string) (*db.CosmeticAdminMembership, int, error)
	RevokeAdminMembership(context.Context, string, string, string) (*db.CosmeticAdminMembership, []string, bool, error)
	ExpireAdminMembershipsForEmail(context.Context, string, time.Time) (int, []string, error)
}

// Authority is the one logical shared authority consumed by Arena. Consumer
// handlers accept the narrow facet they use so tests do not need broad mocks.
type Authority interface {
	IdentityAuthority
	MetadataAuthority
	CosmeticsAuthority
}
