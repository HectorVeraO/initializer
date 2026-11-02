package dispatch

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/fixture/srv/internal/platform/core/api"
	"example.com/fixture/srv/internal/platform/core/reqctx"
)

// maxReferenceLength bounds the reference of a job.
const maxReferenceLength = 64

// referencePattern is the contract's shape of a reference.
var referencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// JobView is the contract's DispatchJob.
type JobView struct {
	ID        string    `json:"id"`
	Reference string    `json:"reference"`
	Status    string    `json:"status"`
	CreatedBy *string   `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// Service holds the dispatch rules. Every error it returns for a caller
// mistake is an *api.Error: a 422 from api.Problems, or a 409 for a reference
// that is already queued.
type Service struct {
	store *store
}

// NewService builds the service over the shared pool. It panics on a nil
// pool: that is a wiring mistake, not a runtime condition.
func NewService(pool *pgxpool.Pool) *Service {
	if pool == nil {
		panic("dispatch: NewService requires a pool")
	}
	return &Service{store: &store{pool: pool}}
}

// Create validates the body and queues one job for the caller.
func (s *Service) Create(ctx context.Context, principal reqctx.Principal, body *api.Body) (JobView, error) {
	var problems api.Problems
	reference, ok := body.String(&problems, "reference", api.Required)
	if ok {
		validateReference(&problems, reference)
	}
	err := problems.Err()
	if err != nil {
		return JobView{}, err
	}
	view, err := s.store.insert(ctx, reference, accountArg(principal))
	if errors.Is(err, errReferenceTaken) {
		return JobView{}, api.NewError(http.StatusConflict, CodeReferenceTaken,
			"A job with this reference is already queued")
	}
	return view, err
}

// List returns one page of the queue ordered by reference.
func (s *Service) List(ctx context.Context, query api.PageQuery) (api.Page[JobView], error) {
	items, total, err := s.store.list(ctx, query.SortDesc, query.Size, query.Offset())
	if err != nil {
		return api.Page[JobView]{}, err
	}
	return api.NewPage(items, query, total), nil
}

// validateReference accumulates every problem of a reference.
func validateReference(problems *api.Problems, reference string) {
	if len(reference) > maxReferenceLength {
		problems.Add("reference", api.CodeFieldTooLong, "Must be at most 64 characters")
	}
	if !referencePattern.MatchString(reference) {
		problems.Add("reference", api.CodeFieldInvalidFmt,
			"Must be letters, digits, dot, dash or underscore, starting with a letter or a digit")
	}
}

// accountArg returns the caller's account as UUID text, or nil when the
// principal carries no account.
func accountArg(principal reqctx.Principal) *string {
	if principal.AccountID == "" {
		return nil
	}
	account := string(principal.AccountID)
	return &account
}
