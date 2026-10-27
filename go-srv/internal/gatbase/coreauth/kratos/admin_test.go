package kratos_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/coreauth/kratos"
)

const identityJSON = `{"id":"ident-1","traits":{"email":"Jane.Doe@example.com"}}`

func TestFindIdentityByEmailReturnsLowerCasedIdentity(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(200, `[`+identityJSON+`]`))
	admin := kratos.NewAdmin(fake.url(t))

	identity, found, err := admin.FindIdentityByEmail(context.Background(), "Jane.Doe@example.com")

	if err != nil || !found {
		t.Fatalf("want found, got found=%v err=%v", found, err)
	}
	if identity.ID != "ident-1" || identity.Email != "jane.doe@example.com" {
		t.Fatalf("unexpected identity %+v", identity)
	}
	req := fake.requests()[0]
	if req.Method != http.MethodGet || req.Path != "/admin/identities" ||
		req.Query.Get("credentials_identifier") != "jane.doe@example.com" ||
		req.Accept != "application/json" {
		t.Fatalf("unexpected request %+v", req)
	}
}

func TestFindIdentityByEmailEmptyListIsNotFound(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(200, `[]`))
	admin := kratos.NewAdmin(fake.url(t))

	_, found, err := admin.FindIdentityByEmail(context.Background(), "nobody@example.com")

	if err != nil || found {
		t.Fatalf("want not found without error, got found=%v err=%v", found, err)
	}
}

func TestFindIdentityByEmailMapsFailuresToUnavailable(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500":               jsonReply(500, `{}`),
		"unexpected status": jsonReply(404, `{}`),
		"closed connection": closeConnection,
		"undecodable":       jsonReply(200, `{`),
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			fake := newFakeKratos(t, handler)
			admin := kratos.NewAdmin(fake.url(t))

			_, _, err := admin.FindIdentityByEmail(context.Background(), "a@example.com")

			if !errors.Is(err, kratos.ErrUnavailable) {
				t.Fatalf("want ErrUnavailable, got %v", err)
			}
		})
	}
}

func TestFindIdentityErrorDoesNotLeakTheEmail(t *testing.T) {
	fake := newFakeKratos(t, closeConnection)
	admin := kratos.NewAdmin(fake.url(t))

	_, _, err := admin.FindIdentityByEmail(context.Background(), "private.person@example.com")

	if err == nil || strings.Contains(err.Error(), "private.person") {
		t.Fatalf("error must exist and not carry the email: %v", err)
	}
}

func TestCreatePasswordIdentitySendsTheDesignedBody(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(201, identityJSON))
	admin := kratos.NewAdmin(fake.url(t))

	identity, err := admin.CreatePasswordIdentity(context.Background(), "Jane.Doe@example.com", "pw-for-test")

	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if identity.ID != "ident-1" || identity.Email != "jane.doe@example.com" {
		t.Fatalf("unexpected identity %+v", identity)
	}
	req := fake.requests()[0]
	if req.Method != http.MethodPost || req.Path != "/admin/identities" ||
		req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected request %+v", req)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	want := `{"credentials":{"password":{"config":{"password":"pw-for-test"}}},` +
		`"schema_id":"default","state":"active","traits":{"email":"jane.doe@example.com"}}`
	canonical, _ := json.Marshal(body)
	if string(canonical) != want {
		t.Fatalf("body = %s", canonical)
	}
}

func TestCreatePasswordIdentityMapsAnswers(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"409", jsonReply(409, `{}`), kratos.ErrIdentityExists},
		{"500", jsonReply(500, `{}`), kratos.ErrUnavailable},
		{"503", jsonReply(503, `{}`), kratos.ErrUnavailable},
		{"closed connection", closeConnection, kratos.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeKratos(t, tc.handler)
			admin := kratos.NewAdmin(fake.url(t))

			_, err := admin.CreatePasswordIdentity(context.Background(), "a@example.com", "pw-for-test")

			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestCreatePasswordIdentityRejectionIsNeitherExistsNorUnavailable(t *testing.T) {
	fake := newFakeKratos(t, jsonReply(400, `{"error":{"message":"bad"}}`))
	admin := kratos.NewAdmin(fake.url(t))

	_, err := admin.CreatePasswordIdentity(context.Background(), "a@example.com", "pw-for-test")

	if !errors.Is(err, kratos.ErrPasswordRejected) || errors.Is(err, kratos.ErrIdentityExists) || errors.Is(err, kratos.ErrUnavailable) {
		t.Fatalf("want ErrPasswordRejected alone, got %v", err)
	}
	if strings.Contains(err.Error(), "pw-for-test") {
		t.Fatal("error carries the password")
	}
}
