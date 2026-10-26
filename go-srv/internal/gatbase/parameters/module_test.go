package parameters_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/parameters"
)

func TestModuleDeclaresItsIdentityAndRoutes(t *testing.T) {
	module := parameters.Module()
	if module.ID != "parameters" || module.Migrations == nil || module.Provide == nil {
		t.Fatalf("module = %+v", module)
	}
	if module.Mount == nil || module.Start != nil {
		t.Fatal("this module must mount routes and start nothing")
	}
	if parameters.MigrationsVersionTable() != "cor_migrations_parameters" {
		t.Fatalf("version table = %s", parameters.MigrationsVersionTable())
	}
}

func TestMigrationsCreateTheSysTables(t *testing.T) {
	_, pool := newService(t)
	rows, err := pool.Query(context.Background(), `SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name LIKE 'sys\_%' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if len(names) != 2 || names[0] != "sys_parameter_overrides" || names[1] != "sys_parameters" {
		t.Fatalf("tables = %v", names)
	}
	var applied int
	err = pool.QueryRow(context.Background(), "SELECT count(*) FROM cor_migrations_parameters WHERE version_id > 0").Scan(&applied)
	if err != nil || applied != 2 {
		t.Fatalf("applied = %d, err = %v", applied, err)
	}
}

const (
	checkViolation      = "23514"
	foreignKeyViolation = "23503"
)

func TestDatabaseRejectsWhatTheRulesForbid(t *testing.T) {
	_, pool := newService(t)
	ctx := context.Background()
	statements := []string{
		`INSERT INTO sys_parameters (key, value_type, default_value) VALUES ('Bad', 'string', '"x"')`,
		`INSERT INTO sys_parameters (key, value_type, default_value) VALUES ('a', 'date', '"x"')`,
		`INSERT INTO sys_parameters (key, value_type, default_value) VALUES ('a', 'json', 'null')`,
		`INSERT INTO sys_parameters (key, value_type, default_value, min_value, max_value) VALUES ('a', 'number', '1', 5, 1)`,
		`INSERT INTO sys_parameters (key, value_type, default_value, allowed_values) VALUES ('a', 'string', '"x"', '{}')`,
		`INSERT INTO sys_parameter_overrides (tenant_id, parameter_key, value) VALUES (gen_random_uuid(), 'missing', '1')`,
	}
	for index, statement := range statements {
		_, err := pool.Exec(ctx, statement)
		wantCode := checkViolation
		if index == len(statements)-1 {
			wantCode = foreignKeyViolation
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != wantCode {
			t.Errorf("%s: want SQLSTATE %s, got %v", statement, wantCode, err)
		}
	}
}
