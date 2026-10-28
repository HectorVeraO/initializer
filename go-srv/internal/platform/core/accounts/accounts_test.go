package accounts_test

import (
	"context"
	"errors"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/accounts"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

func TestNoDirectoryFindsNobody(t *testing.T) {
	ctx := context.Background()
	ids := []reqctx.AccountID{"11111111-1111-4111-8111-111111111111", "not-a-uuid", ""}
	for name, input := range map[string][]reqctx.AccountID{"ids": ids, "nil": nil, "empty": {}} {
		found, err := accounts.NoDirectory.Get(ctx, input)
		if err != nil || found == nil || len(found) != 0 {
			t.Errorf("Get(%s) = %#v, %v; want an empty non-nil map", name, found, err)
		}
	}
	for _, email := range []string{"user@example.com", "", "  UPPER@example.com "} {
		list, err := accounts.NoDirectory.ByEmail(ctx, email)
		if err != nil || list == nil || len(list) != 0 {
			t.Errorf("ByEmail(%q) = %#v, %v; want an empty non-nil slice", email, list, err)
		}
	}
}

func TestNoProvisionerRefusesOperationally(t *testing.T) {
	ref, err := accounts.NoProvisioner.Provision(context.Background(), accounts.NewAccount{
		Email: "user@example.com", Password: "synthetic-pass-phrase", FullName: "Jane Doe",
	})
	if !errors.Is(err, accounts.ErrNoProvisioner) {
		t.Fatalf("Provision error = %v; want ErrNoProvisioner", err)
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		t.Fatalf("ErrNoProvisioner must not be a wire rejection: %+v", apiErr)
	}
	if ref.ID != "" || ref.Email != "" {
		t.Fatalf("Provision returned %+v with an error", ref)
	}
}

func TestNoProvisionerGrantsOperatorToEveryEmail(t *testing.T) {
	for _, email := range []string{"user@example.com", "", "UPPER@example.com", "not an email"} {
		if !accounts.NoProvisioner.GrantsOperator(email) {
			t.Errorf("GrantsOperator(%q) = false; the default fails closed", email)
		}
	}
}
