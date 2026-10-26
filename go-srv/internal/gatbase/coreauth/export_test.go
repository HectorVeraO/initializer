package coreauth

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewTestPool gives the external tests an it_ database with the module
// migrations applied.
func NewTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	return newTestPool(t)
}
