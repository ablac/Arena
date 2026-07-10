package db

import (
	"errors"
	"testing"
)

func TestNormalizeCustomerEmail(t *testing.T) {
	normalized, err := NormalizeCustomerEmail("  Owner+Arena@Example.COM  ")
	if err != nil || normalized != "owner+arena@example.com" {
		t.Fatalf("NormalizeCustomerEmail = (%q, %v)", normalized, err)
	}
	for _, invalid := range []string{"", "owner", "Owner <owner@example.com>", "owner@example.com,other@example.com"} {
		if _, err := NormalizeCustomerEmail(invalid); !errors.Is(err, ErrCustomerEmailInvalid) {
			t.Errorf("NormalizeCustomerEmail(%q) error = %v, want ErrCustomerEmailInvalid", invalid, err)
		}
	}
}

func TestCustomerCosmeticsQueriesNilPoolReturnError(t *testing.T) {
	original := Pool
	Pool = nil
	t.Cleanup(func() { Pool = original })

	if _, err := GetCustomerAccount(t.Context(), "account"); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("GetCustomerAccount error = %v, want ErrNoDatabase", err)
	}
	if _, _, err := GrantCosmeticLicense(t.Context(), "owner@example.com", "skin", "manual", ""); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("GrantCosmeticLicense error = %v, want ErrNoDatabase", err)
	}
	if _, err := AssignCosmeticLicense(t.Context(), "account", "license", nil); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("AssignCosmeticLicense error = %v, want ErrNoDatabase", err)
	}
}
