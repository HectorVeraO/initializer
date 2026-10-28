package kratos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const adminTimeout = 5 * time.Second

// Identity is an admin-API identity.
type Identity struct {
	ID string
	// Email is identity.traits.email, lower-cased.
	Email string
}

// Admin calls the Kratos admin API.
type Admin struct {
	base   *url.URL
	client *http.Client
}

// NewAdmin returns a client for the Kratos admin API at baseURL.
func NewAdmin(baseURL *url.URL, opts ...Option) *Admin {
	return &Admin{base: baseURL, client: newHTTPClient(adminTimeout, opts)}
}

type identityBody struct {
	ID     string `json:"id"`
	Traits struct {
		Email string `json:"email"`
	} `json:"traits"`
}

func (b identityBody) identity() Identity {
	return Identity{ID: b.ID, Email: strings.ToLower(b.Traits.Email)}
}

// FindIdentityByEmail looks an identity up by its password identifier. The
// bool is false when none exists.
func (a *Admin) FindIdentityByEmail(ctx context.Context, email string) (Identity, bool, error) {
	target := endpoint(a.base, "/admin/identities")
	target.RawQuery = url.Values{"credentials_identifier": {strings.ToLower(email)}}.Encode()
	req, err := newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Identity{}, false, err
	}
	status, body, err := do(a.client, req)
	if err != nil {
		return Identity{}, false, err
	}
	if status != http.StatusOK {
		return Identity{}, false, fmt.Errorf("%w: find answered %d", ErrUnavailable, status)
	}
	var items []identityBody
	if err := decode(body, &items); err != nil {
		return Identity{}, false, err
	}
	if len(items) == 0 {
		return Identity{}, false, nil
	}
	return items[0].identity(), true, nil
}

// CreatePasswordIdentity creates an active identity with a password
// credential; the email is lower-cased like the lookup key. It returns ErrIdentityExists when the email is taken. The
// password is only written to the request body.
func (a *Admin) CreatePasswordIdentity(ctx context.Context, email, password string) (Identity, error) {
	payload := map[string]any{
		"schema_id": "default",
		"traits":    map[string]string{"email": strings.ToLower(email)},
		"credentials": map[string]any{
			"password": map[string]any{
				"config": map[string]string{"password": password},
			},
		},
		"state": "active",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Identity{}, fmt.Errorf("kratos: encoding identity: %w", err)
	}
	req, err := newRequest(ctx, http.MethodPost, endpoint(a.base, "/admin/identities"), bytes.NewReader(encoded))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	status, body, err := do(a.client, req)
	if err != nil {
		return Identity{}, err
	}
	switch {
	case status == http.StatusConflict:
		return Identity{}, ErrIdentityExists
	case status == http.StatusBadRequest:
		return Identity{}, ErrPasswordRejected
	case status >= http.StatusInternalServerError:
		return Identity{}, fmt.Errorf("%w: create answered %d", ErrUnavailable, status)
	case status != http.StatusCreated:
		return Identity{}, fmt.Errorf("kratos: create identity answered %d", status)
	}
	var parsed identityBody
	if err := decode(body, &parsed); err != nil {
		return Identity{}, err
	}
	return parsed.identity(), nil
}

// DeleteIdentity removes an identity; one that is already gone counts as
// deleted.
func (a *Admin) DeleteIdentity(ctx context.Context, id string) error {
	req, err := newRequest(ctx, http.MethodDelete, endpoint(a.base, "/admin/identities/"+url.PathEscape(id)), nil)
	if err != nil {
		return err
	}
	status, _, err := do(a.client, req)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusNoContent || status == http.StatusNotFound:
		return nil
	case status >= http.StatusInternalServerError:
		return fmt.Errorf("%w: delete answered %d", ErrUnavailable, status)
	}
	return fmt.Errorf("kratos: delete identity answered %d", status)
}
