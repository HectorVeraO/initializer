// Package db opens the pgx pool and offers transactions and error classification.
package db

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
)

const (
	pingTimeout     = 5 * time.Second
	rollbackTimeout = 5 * time.Second
	uniqueViolation = "23505"
)

// Open parses url (pgxpool.ParseConfig), sets MaxConns and creates the pool.
// It does not connect (pgxpool.NewWithConfig is lazy); the error is a parse
// error whose text never contains the URL.
func Open(ctx context.Context, url config.Secret, maxConns int) (*pgxpool.Pool, error) {
	if maxConns < 1 || maxConns > math.MaxInt32 {
		return nil, fmt.Errorf("db: maxConns must be between 1 and %d", math.MaxInt32)
	}
	cfg, err := pgxpool.ParseConfig(url.Reveal())
	if err != nil {
		// pgx error text can quote parts of the URL; report only that it is invalid.
		return nil, errors.New("db: the database URL cannot be parsed")
	}
	cfg.MaxConns = int32(maxConns)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("db: cannot create the connection pool")
	}
	return pool, nil
}

// Ping acquires a connection and pings it with a 5 s timeout.
func Ping(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("db: Ping requires a pool")
	}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: acquire connection: %w", err)
	}
	defer conn.Release()
	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	return nil
}

// InTx runs fn in one transaction: commit on nil, rollback otherwise (a
// rollback error is joined and never hides fn's error).
func InTx(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions, fn func(pgx.Tx) error) error {
	if pool == nil || fn == nil {
		return errors.New("db: InTx requires a pool and a function")
	}
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}
	fnErr := runGuarded(tx, fn)
	if fnErr != nil {
		rollbackErr := rollback(tx)
		if rollbackErr == nil {
			return fnErr
		}
		return errors.Join(fnErr, rollbackErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}

// runGuarded rolls the transaction back before a panic of fn propagates.
func runGuarded(tx pgx.Tx, fn func(pgx.Tx) error) error {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = rollback(tx)
			panic(recovered)
		}
	}()
	return fn(tx)
}

// rollback runs detached from the caller's cancellation so a canceled request still rolls back.
func rollback(tx pgx.Tx) error {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	err := tx.Rollback(ctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("db: rollback: %w", err)
	}
	return nil
}

// IsUniqueViolation reports SQLSTATE 23505 (errors.As on *pgconn.PgError).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	isPgError := errors.As(err, &pgErr)
	return isPgError && pgErr.Code == uniqueViolation
}
