package parameters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/api"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/db"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
)

// Sources of an effective value.
const (
	sourceGlobal = "global"
	sourceTenant = "tenant"
)

// View is the contract's ParameterView, resolved for one tenant.
type View struct {
	Key              string            `json:"key"`
	ValueType        string            `json:"valueType"`
	EffectiveValue   json.RawMessage   `json:"effectiveValue"`
	DefaultValue     json.RawMessage   `json:"defaultValue"`
	MinValue         *float64          `json:"minValue"`
	MaxValue         *float64          `json:"maxValue"`
	AllowedValues    []json.RawMessage `json:"allowedValues"`
	Description      string            `json:"description"`
	IsSystemRequired bool              `json:"isSystemRequired"`
	Source           string            `json:"source"`
}

// Deleted is the contract's ParameterDeleted.
type Deleted struct {
	Key                  string `json:"key"`
	DeletedOverrideCount int    `json:"deletedOverrideCount"`
}

// Service holds the parameters rules. Every error it returns for a caller
// mistake is an *api.Error: api.NotFound(), 409 or a 422 from api.Problems.
type Service struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	clk    clock.Clock
	store  *store
}

// NewService builds the service. It panics on a nil argument: that is a
// wiring mistake, not a runtime condition.
func NewService(pool *pgxpool.Pool, logger *slog.Logger, clk clock.Clock) *Service {
	if pool == nil || logger == nil || clk == nil {
		panic("parameters: NewService requires a pool, a logger and a clock")
	}
	return &Service{pool: pool, logger: logger, clk: clk, store: &store{pool: pool}}
}

// buildView resolves a definition for a tenant whose override is override
// (nil: none).
func buildView(d definition, override json.RawMessage) View {
	view := View{
		Key:              d.Key,
		ValueType:        string(d.ValueType),
		EffectiveValue:   d.DefaultValue,
		DefaultValue:     d.DefaultValue,
		MinValue:         d.Min,
		MaxValue:         d.Max,
		AllowedValues:    d.Allowed,
		Description:      d.Description,
		IsSystemRequired: d.IsSystemRequired,
		Source:           sourceGlobal,
	}
	if override != nil {
		view.EffectiveValue = override
		view.Source = sourceTenant
	}
	return view
}

// tenantArg returns the active tenant as UUID text, or nil when there is none.
func tenantArg(p reqctx.Principal) (*string, error) {
	if p.TenantID == nil {
		return nil, nil
	}
	canonical, ok := api.ParseUUID(string(*p.TenantID))
	if !ok {
		return nil, errors.New("parameters: the principal tenant id is not a UUID")
	}
	return &canonical, nil
}

// accountArg returns the actor account as UUID text, or nil for an actor
// without an account (an API key).
func accountArg(p reqctx.Principal) (*string, error) {
	if p.AccountID == "" {
		return nil, nil
	}
	canonical, ok := api.ParseUUID(string(p.AccountID))
	if !ok {
		return nil, errors.New("parameters: the principal account id is not a UUID")
	}
	return &canonical, nil
}

// requireTenant is for the override methods: the route policy guarantees a
// tenant, so its absence is a programmer error.
func requireTenant(p reqctx.Principal) (string, error) {
	tenant, err := tenantArg(p)
	if err != nil {
		return "", err
	}
	if tenant == nil {
		return "", errors.New("parameters: an override call needs an active tenant")
	}
	return *tenant, nil
}

// overrideOf finds the override of tenant (nil: none) in overrides.
func overrideOf(overrides []tenantValue, tenant *string) json.RawMessage {
	if tenant == nil {
		return nil
	}
	for _, item := range overrides {
		if item.tenantID == *tenant {
			return item.value
		}
	}
	return nil
}

// finish keeps an *api.Error as it is and wraps anything else.
func finish(operation string, err error) error {
	if err == nil {
		return nil
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// inTx runs fn in a transaction.
func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return db.InTx(ctx, s.pool, pgx.TxOptions{}, fn)
}

// loadOrNotFound loads a definition; a malformed or unknown key is a 404.
func (s *Service) loadOrNotFound(ctx context.Context, tx pgx.Tx, key string, mode lockMode) (definition, error) {
	if !validKey(key) {
		return definition{}, api.NotFound()
	}
	d, err := s.store.load(ctx, tx, key, mode)
	if errors.Is(err, errNotFound) {
		return definition{}, api.NotFound()
	}
	return d, err
}

// List returns one page of parameters resolved for the principal's tenant.
func (s *Service) List(ctx context.Context, p reqctx.Principal, q api.PageQuery) (api.Page[View], error) {
	tenant, err := tenantArg(p)
	if err != nil {
		return api.Page[View]{}, err
	}
	isKeySort := q.SortField == "" || q.SortField == "key"
	if !isKeySort || q.Page < 1 || q.Size < 1 {
		return api.Page[View]{}, errors.New("parameters: List needs a parsed page query sorted by key")
	}
	rows, total, err := s.store.list(ctx, tenant, q.SortDesc, q.Size, q.Offset())
	if err != nil {
		return api.Page[View]{}, err
	}
	views := make([]View, 0, len(rows))
	for _, row := range rows {
		views = append(views, buildView(row.def, row.override))
	}
	return api.NewPage(views, q, total), nil
}

// Create validates the body and inserts a definition.
func (s *Service) Create(ctx context.Context, p reqctx.Principal, b *api.Body) (View, error) {
	account, err := accountArg(p)
	if err != nil {
		return View{}, err
	}
	d, err := parseCreate(b)
	if err != nil {
		return View{}, finish("create parameter", err)
	}
	now := s.clk.Now()
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		return s.store.insert(ctx, tx, d, account, now)
	})
	if db.IsUniqueViolation(err) {
		return View{}, api.NewError(http.StatusConflict, CodeParameterAlreadyExists, "A parameter with this key already exists")
	}
	if err != nil {
		return View{}, finish("create parameter", err)
	}
	s.logger.InfoContext(ctx, "parameter_created", slog.String("parameter_key", d.Key))
	return buildView(d, nil), nil
}

// Replace rewrites the mutable fields of a definition. It refuses a change
// that would leave an existing tenant override outside the new constraints.
func (s *Service) Replace(ctx context.Context, p reqctx.Principal, key string, b *api.Body) (View, error) {
	tenant, err := tenantArg(p)
	if err != nil {
		return View{}, err
	}
	account, err := accountArg(p)
	if err != nil {
		return View{}, err
	}
	now := s.clk.Now()
	var view View
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		stored, loadErr := s.loadOrNotFound(ctx, tx, key, lockUpdate)
		if loadErr != nil {
			return loadErr
		}
		next, parseErr := parseReplace(b, stored)
		if parseErr != nil {
			return parseErr
		}
		overrides, listErr := s.store.listOverrides(ctx, tx, key)
		if listErr != nil {
			return listErr
		}
		violating := 0
		rules := next.constraints()
		for _, item := range overrides {
			if checkValue(rules, item.value) != nil {
				violating++
			}
		}
		if violating > 0 {
			message := fmt.Sprintf("%d tenant override(s) would violate the new constraints", violating)
			return api.NewError(http.StatusConflict, CodeOverridesViolateConstraints, message)
		}
		updateErr := s.store.update(ctx, tx, next, account, now)
		if updateErr != nil {
			return updateErr
		}
		view = buildView(next, overrideOf(overrides, tenant))
		return nil
	})
	if err != nil {
		return View{}, finish("replace parameter", err)
	}
	s.logger.InfoContext(ctx, "parameter_replaced", slog.String("parameter_key", key))
	return view, nil
}

// Delete removes a definition and, by cascade, its overrides. A
// system-required parameter is protected.
func (s *Service) Delete(ctx context.Context, _ reqctx.Principal, key string) (Deleted, error) {
	var result Deleted
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		stored, loadErr := s.loadOrNotFound(ctx, tx, key, lockUpdate)
		if loadErr != nil {
			return loadErr
		}
		if stored.IsSystemRequired {
			return api.NewError(http.StatusConflict, CodeSystemParameterProtected, "A system-required parameter cannot be deleted")
		}
		count, countErr := s.store.countOverrides(ctx, tx, key)
		if countErr != nil {
			return countErr
		}
		deleteErr := s.store.deleteDefinition(ctx, tx, key)
		if deleteErr != nil {
			return deleteErr
		}
		result = Deleted{Key: key, DeletedOverrideCount: count}
		return nil
	})
	if err != nil {
		return Deleted{}, finish("delete parameter", err)
	}
	s.logger.InfoContext(ctx, "parameter_deleted",
		slog.String("parameter_key", key), slog.Int("deleted_override_count", result.DeletedOverrideCount))
	return result, nil
}

// SetOverride sets the active tenant's value of a parameter. The tenant comes
// only from the principal.
func (s *Service) SetOverride(ctx context.Context, p reqctx.Principal, key string, b *api.Body) (View, error) {
	tenant, err := requireTenant(p)
	if err != nil {
		return View{}, err
	}
	account, err := accountArg(p)
	if err != nil {
		return View{}, err
	}
	now := s.clk.Now()
	var view View
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		stored, loadErr := s.loadOrNotFound(ctx, tx, key, lockShare)
		if loadErr != nil {
			return loadErr
		}
		raw, _ := b.Raw("value")
		var problems api.Problems
		failure := checkValue(stored.constraints(), raw)
		if failure != nil {
			problems.Add("value", failure.code, failure.message)
		}
		validationErr := problems.Err()
		if validationErr != nil {
			return validationErr
		}
		upsertErr := s.store.upsertOverride(ctx, tx, tenant, key, raw, account, now)
		if upsertErr != nil {
			return upsertErr
		}
		view = buildView(stored, raw)
		return nil
	})
	if err != nil {
		return View{}, finish("set parameter override", err)
	}
	s.logger.InfoContext(ctx, "parameter_override_set", slog.String("parameter_key", key))
	return view, nil
}

// ClearOverride removes the active tenant's value of a parameter; it is
// idempotent and never touches another tenant's row.
func (s *Service) ClearOverride(ctx context.Context, p reqctx.Principal, key string) (View, error) {
	tenant, err := requireTenant(p)
	if err != nil {
		return View{}, err
	}
	var view View
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		stored, loadErr := s.loadOrNotFound(ctx, tx, key, lockShare)
		if loadErr != nil {
			return loadErr
		}
		deleteErr := s.store.deleteOverride(ctx, tx, tenant, key)
		if deleteErr != nil {
			return deleteErr
		}
		view = buildView(stored, nil)
		return nil
	})
	if err != nil {
		return View{}, finish("clear parameter override", err)
	}
	s.logger.InfoContext(ctx, "parameter_override_cleared", slog.String("parameter_key", key))
	return view, nil
}
