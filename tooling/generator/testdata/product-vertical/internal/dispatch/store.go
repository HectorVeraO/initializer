package dispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// errReferenceTaken is returned by an insert whose reference is already
// queued.
var errReferenceTaken = errors.New("dispatch: reference taken")

// columnList is the select list every job read shares.
const columnList = `j.id::text, j.reference, j.status, j.created_by::text, j.created_at`

// store runs the queries on the dsp_jobs table.
type store struct {
	pool *pgxpool.Pool
}

// rowScanner is one row of a query or of a RETURNING clause.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanJob reads columnList into a job.
func scanJob(row rowScanner) (JobView, error) {
	var view JobView
	err := row.Scan(&view.ID, &view.Reference, &view.Status, &view.CreatedBy, &view.CreatedAt)
	if err != nil {
		return JobView{}, err
	}
	return view, nil
}

// insert queues one job; a taken reference is the conflict a caller retries
// with another reference.
func (s *store) insert(ctx context.Context, reference string, createdBy *string) (JobView, error) {
	query := `INSERT INTO dsp_jobs AS j (reference, created_by) VALUES ($1, $2::uuid)
		ON CONFLICT (reference) DO NOTHING
		RETURNING ` + columnList
	view, err := scanJob(s.pool.QueryRow(ctx, query, reference, createdBy))
	if errors.Is(err, pgx.ErrNoRows) {
		return JobView{}, errReferenceTaken
	}
	if err != nil {
		return JobView{}, fmt.Errorf("insert job: %w", err)
	}
	return view, nil
}

// list returns one page of jobs ordered by reference, and the total number of
// jobs.
func (s *store) list(ctx context.Context, isDescending bool, limit, offset int) ([]JobView, int64, error) {
	direction := "ASC"
	if isDescending {
		direction = "DESC"
	}
	// COLLATE "C" orders references by byte value, so the order does not
	// depend on the database locale.
	query := `SELECT ` + columnList + ` FROM dsp_jobs j ORDER BY j.reference COLLATE "C" ` +
		direction + ` LIMIT $1 OFFSET $2`
	rows, err := s.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	var items []JobView
	for rows.Next() {
		view, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("scan job: %w", scanErr)
		}
		items = append(items, view)
	}
	err = rows.Err()
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	var total int64
	err = s.pool.QueryRow(ctx, "SELECT count(*) FROM dsp_jobs").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}
	return items, total, nil
}
