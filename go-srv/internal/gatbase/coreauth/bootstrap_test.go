package coreauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
)

const (
	testSlug       = "acme"
	testAdminEmail = "admin@example.com"
	testJaneEmail  = "jane.doe@example.com"
)

// fakeRequest is one request the fake Kratos admin API received.
type fakeRequest struct {
	Method      string
	Path        string
	Query       string
	Traceparent string
	Body        string
}

// fakeKratos is the Kratos admin API at the HTTP boundary: a lookup by
// credentials_identifier and an identity create that answers 409 for a taken
// email.
type fakeKratos struct {
	server *httptest.Server

	mu         sync.Mutex
	identities map[string]string
	requests   []fakeRequest
	// failures is how many requests still answer 503 before the fake recovers.
	failures int
	// rejectStatus, when set, is the status every create answers.
	rejectStatus int
	// hideFirstFind makes the first lookup answer an empty list, so a create
	// of a taken email meets the 409.
	hideFirstFind bool
}

func newFakeKratos(t testing.TB) *fakeKratos {
	t.Helper()
	fake := &fakeKratos{identities: map[string]string{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeKratos) serve(w http.ResponseWriter, r *http.Request) {
	var body bytes.Buffer
	_, _ = body.ReadFrom(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, fakeRequest{
		Method:      r.Method,
		Path:        r.URL.Path,
		Query:       r.URL.RawQuery,
		Traceparent: r.Header.Get("Traceparent"),
		Body:        body.String(),
	})
	if f.rejectStatus != 0 && r.Method == http.MethodPost {
		w.WriteHeader(f.rejectStatus)
		return
	}
	if f.failures > 0 {
		f.failures--
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	isFind := r.Method == http.MethodGet && r.URL.Path == "/admin/identities"
	isCreate := r.Method == http.MethodPost && r.URL.Path == "/admin/identities"
	switch {
	case isFind:
		f.find(w, r.URL.Query().Get("credentials_identifier"))
	case isCreate:
		f.create(w, body.Bytes())
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeKratos) find(w http.ResponseWriter, email string) {
	id, found := f.identities[email]
	if f.hideFirstFind {
		f.hideFirstFind = false
		found = false
	}
	items := []map[string]any{}
	if found {
		items = append(items, map[string]any{"id": id, "traits": map[string]string{"email": email}})
	}
	_ = json.NewEncoder(w).Encode(items)
}

func (f *fakeKratos) create(w http.ResponseWriter, raw []byte) {
	var payload struct {
		Traits struct {
			Email string `json:"email"`
		} `json:"traits"`
	}
	_ = json.Unmarshal(raw, &payload)
	email := payload.Traits.Email
	if _, taken := f.identities[email]; taken {
		w.WriteHeader(http.StatusConflict)
		return
	}
	id := newUUID()
	f.identities[email] = id
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "traits": map[string]string{"email": email}})
}

func (f *fakeKratos) addIdentity(email string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := newUUID()
	f.identities[email] = id
	return id
}

func (f *fakeKratos) received() []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeRequest(nil), f.requests...)
}

func (f *fakeKratos) count(method string) int {
	total := 0
	for _, req := range f.received() {
		if req.Method == method {
			total++
		}
	}
	return total
}

// newUUID returns a random UUID without a testing handle, for the fake's
// serving goroutine.
func newUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexDigits[v>>4], hexDigits[v&0x0f])
	}
	return string(out)
}

// randomPassword is 20 random alphanumeric characters, built at test time.
func randomPassword(t testing.TB) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := make([]byte, 20)
	_, err := rand.Read(raw)
	if err != nil {
		t.Fatalf("random: %v", err)
	}
	for i, v := range raw {
		raw[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(raw)
}

// syncBuffer is a log sink safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// events returns every log record whose msg is name.
func (b *syncBuffer) events(t testing.TB, name string) []map[string]any {
	t.Helper()
	var found []map[string]any
	for _, line := range strings.Split(b.String(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		err := json.Unmarshal([]byte(line), &record)
		if err != nil {
			t.Fatalf("log line is not JSON: %v", err)
		}
		if record["msg"] == name {
			found = append(found, record)
		}
	}
	return found
}

// bootstrapEnv is one test's world: a database, a fake Kratos, a log sink and
// the admin password.
type bootstrapEnv struct {
	t        *testing.T
	pool     *pgxpool.Pool
	fake     *fakeKratos
	logs     *syncBuffer
	password string
}

func newBootstrapEnv(t *testing.T) *bootstrapEnv {
	t.Helper()
	return &bootstrapEnv{
		t:        t,
		pool:     newTestPool(t),
		fake:     newFakeKratos(t),
		logs:     &syncBuffer{},
		password: randomPassword(t),
	}
}

// settings are the bootstrap variables plus the session keys.
func (e *bootstrapEnv) settings(extra map[string]string) map[string]string {
	settings := map[string]string{
		"GATBASE_PUBLIC_BASE_URL":          "http://localhost:10120",
		"GATBASE_SESSION_KEYS":             randomSessionKeys(e.t),
		"GATBASE_KRATOS_ADMIN_URL":         e.fake.server.URL,
		"GATBASE_BOOTSTRAP_TENANT_SLUG":    testSlug,
		"GATBASE_BOOTSTRAP_ADMIN_EMAIL":    testAdminEmail,
		"GATBASE_BOOTSTRAP_ADMIN_PASSWORD": e.password,
	}
	for key, value := range extra {
		if value == "" {
			delete(settings, key)
			continue
		}
		settings[key] = value
	}
	return settings
}

// build runs core.Build with the core-auth module; a started app is closed on cleanup.
func (e *bootstrapEnv) build(extra map[string]string) (*core.App, error) {
	e.t.Helper()
	return e.buildWith(context.Background(), extra)
}

func (e *bootstrapEnv) buildWith(ctx context.Context, extra map[string]string) (*core.App, error) {
	e.t.Helper()
	app, err := core.Build(ctx, core.Options{
		Logger: slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Config: config.MapSource(e.settings(extra)),
		Clock:  clock.System{},
		Pool:   e.pool,
	}, []core.Module{Module()})
	if err == nil {
		e.t.Cleanup(func() { _ = app.Close(context.Background()) })
	}
	return app, err
}

func (e *bootstrapEnv) mustBuild(extra map[string]string) {
	e.t.Helper()
	_, err := e.build(extra)
	if err != nil {
		e.t.Fatalf("Build: %v", err)
	}
}

func (e *bootstrapEnv) seedAccount(identityID, email string) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(),
		"INSERT INTO iam_accounts (kratos_identity_id, email) VALUES ($1::uuid, lower($2))", identityID, email)
	if err != nil {
		e.t.Fatalf("seed account %s: %v", email, err)
	}
}

func (e *bootstrapEnv) seedTenant(slug, name string) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(), "INSERT INTO ten_tenants (slug, name) VALUES ($1, $2)", slug, name)
	if err != nil {
		e.t.Fatalf("seed tenant %s: %v", slug, err)
	}
}

func (e *bootstrapEnv) count(table string) int {
	e.t.Helper()
	var total int
	err := e.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&total)
	if err != nil {
		e.t.Fatalf("count %s: %v", table, err)
	}
	return total
}

// advisoryLockHolders counts the granted bootstrap locks in this database,
// read from pg_locks so a re-entrant lock on the same session cannot hide a leak.
func (e *bootstrapEnv) advisoryLockHolders() int {
	e.t.Helper()
	var holders int
	err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_locks
 WHERE locktype = 'advisory' AND granted
 AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
 AND ((classid::bigint << 32) | objid::bigint) = $1`, bootstrapLockKey).Scan(&holders)
	if err != nil {
		e.t.Fatalf("read pg_locks: %v", err)
	}
	return holders
}

func (e *bootstrapEnv) assertNoSecretInLogs() {
	e.t.Helper()
	text := strings.ToLower(e.logs.String())
	for _, secret := range []string{strings.ToLower(e.password), testAdminEmail} {
		if strings.Contains(text, secret) {
			e.t.Errorf("a log line contains %q", secret)
		}
	}
}

func (e *bootstrapEnv) account(email string) Account {
	e.t.Helper()
	var account Account
	row := e.pool.QueryRow(context.Background(),
		"SELECT "+accountColumns+" FROM iam_accounts WHERE email = $1", email)
	account, err := scanAccount(row)
	if err != nil {
		e.t.Fatalf("account %s: %v", email, err)
	}
	return account
}

func TestBootstrapCreatesTheTenantAndItsOperatorAdmin(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(nil)

	if env.count("ten_tenants") != 1 || env.count("iam_accounts") != 1 || env.count("ten_memberships") != 1 {
		t.Fatalf("rows: %d tenants, %d accounts, %d memberships; want 1 each",
			env.count("ten_tenants"), env.count("iam_accounts"), env.count("ten_memberships"))
	}
	st := newStore(env.pool, clock.System{})
	tenant, err := st.TenantBySlug(context.Background(), testSlug)
	if err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if tenant.Name != testSlug || tenant.Status != "active" {
		t.Errorf("tenant name %q status %q", tenant.Name, tenant.Status)
	}
	account := env.account(testAdminEmail)
	if !account.IsPlatformOperator {
		t.Error("the bootstrap admin is not a platform operator")
	}
	if account.LastActiveTenantID == nil || *account.LastActiveTenantID != tenant.ID {
		t.Errorf("last_active_tenant_id = %v, want %s", account.LastActiveTenantID, tenant.ID)
	}
}

func TestBootstrapPromotesAnAccountThatAlreadyExistsForTheAdminIdentity(t *testing.T) {
	env := newBootstrapEnv(t)
	identityID := env.fake.addIdentity(testAdminEmail)
	env.seedAccount(identityID, testAdminEmail)
	if env.account(testAdminEmail).IsPlatformOperator {
		t.Fatal("the seeded account is already an operator")
	}

	env.mustBuild(nil)

	if env.count("iam_accounts") != 1 || env.count("ten_tenants") != 1 || env.count("ten_memberships") != 1 {
		t.Errorf("rows: %d accounts, %d tenants, %d memberships; want 1 each",
			env.count("iam_accounts"), env.count("ten_tenants"), env.count("ten_memberships"))
	}
	account := env.account(testAdminEmail)
	if !account.IsPlatformOperator || account.LastActiveTenantID == nil {
		t.Errorf("account operator=%v lastActiveTenant=%v, want operator with a tenant",
			account.IsPlatformOperator, account.LastActiveTenantID)
	}
}

func TestBootstrapMakesTheAdminAnActiveAdminMemberOfTheTenant(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(nil)

	st := newStore(env.pool, clock.System{})
	tenant, err := st.TenantBySlug(context.Background(), testSlug)
	if err != nil {
		t.Fatalf("tenant: %v", err)
	}
	account := env.account(testAdminEmail)
	membership, err := st.MembershipByTenantAccount(context.Background(), tenant.ID, account.ID)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	if membership.Role != "admin" || membership.Status != "active" {
		t.Errorf("membership role %q status %q", membership.Role, membership.Status)
	}
}

func TestBootstrapLogsTheCreationWithoutSecrets(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(nil)

	account := env.account(testAdminEmail)
	created := env.logs.events(t, "bootstrap_admin_created")
	if len(created) != 1 || created[0]["account_id"] != account.ID {
		t.Errorf("bootstrap_admin_created = %v", created)
	}
	env.assertNoSecretInLogs()
}

func TestBootstrapSendsTheIdentityToKratosWithATraceparent(t *testing.T) {
	env := newBootstrapEnv(t)
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	_, err = env.buildWith(trace.ContextWithSpanContext(context.Background(), spanContext), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	requests := env.fake.received()
	var lookup, create *fakeRequest
	for i := range requests {
		switch requests[i].Method {
		case http.MethodGet:
			lookup = &requests[i]
		case http.MethodPost:
			create = &requests[i]
		}
	}
	if lookup == nil || create == nil {
		t.Fatalf("Kratos requests = %+v, want a lookup and a create", requests)
	}
	if !strings.Contains(lookup.Query, "credentials_identifier=admin%40example.com") {
		t.Errorf("lookup query = %q", lookup.Query)
	}
	var body struct {
		SchemaID    string `json:"schema_id"`
		State       string `json:"state"`
		Traits      struct{ Email string }
		Credentials struct {
			Password struct {
				Config struct{ Password string }
			}
		}
	}
	err = json.Unmarshal([]byte(create.Body), &body)
	if err != nil {
		t.Fatalf("create body: %v", err)
	}
	if body.SchemaID != "default" || body.State != "active" || body.Traits.Email != testAdminEmail ||
		body.Credentials.Password.Config.Password != env.password {
		t.Errorf("create body does not carry the configured identity: %s", create.Body)
	}
	for _, req := range requests {
		parts := strings.Split(req.Traceparent, "-")
		if len(parts) != 4 || parts[1] != traceID.String() {
			t.Errorf("%s %s traceparent = %q, want trace id %s", req.Method, req.Path, req.Traceparent, traceID)
		}
	}
}

func TestSecondSequentialRunSkipsAndChangesNothing(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(nil)
	env.mustBuild(map[string]string{"GATBASE_BOOTSTRAP_TENANT_SLUG": "other-corp"})

	if env.count("ten_tenants") != 1 || env.count("iam_accounts") != 1 || env.count("ten_memberships") != 1 {
		t.Fatalf("a second run changed the rows")
	}
	skipped := env.logs.events(t, "bootstrap_skipped")
	if len(skipped) != 1 || skipped[0]["reason"] != "tenants_exist" {
		t.Errorf("bootstrap_skipped = %v", skipped)
	}
	if env.fake.count(http.MethodPost) != 1 {
		t.Errorf("Kratos saw %d creates, want 1", env.fake.count(http.MethodPost))
	}
}

func TestTwoConcurrentRunsLeaveOneTenantOneAccountOneMembership(t *testing.T) {
	env := newBootstrapEnv(t)
	const runs = 2
	errs := make([]error, runs)
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = env.build(nil)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if env.count("ten_tenants") != 1 || env.count("iam_accounts") != 1 || env.count("ten_memberships") != 1 {
		t.Fatalf("rows: %d tenants, %d accounts, %d memberships; want 1 each",
			env.count("ten_tenants"), env.count("iam_accounts"), env.count("ten_memberships"))
	}
	if env.fake.count(http.MethodPost) != 1 {
		t.Errorf("Kratos saw %d creates, want 1", env.fake.count(http.MethodPost))
	}
	if len(env.logs.events(t, "bootstrap_admin_created")) != 1 || len(env.logs.events(t, "bootstrap_skipped")) != 1 {
		t.Errorf("want one creation and one skip in the logs")
	}
}

func TestAnIdentityLeftByAFailedRunIsReusedWithoutACreate(t *testing.T) {
	env := newBootstrapEnv(t)
	identityID := env.fake.addIdentity(testAdminEmail)
	env.mustBuild(nil)

	if env.fake.count(http.MethodPost) != 0 {
		t.Errorf("Kratos saw %d creates, want 0", env.fake.count(http.MethodPost))
	}
	for _, req := range env.fake.received() {
		if req.Method != http.MethodGet {
			t.Errorf("unexpected %s %s: an existing identity is never changed", req.Method, req.Path)
		}
	}
	account := env.account(testAdminEmail)
	if account.KratosIdentityID != identityID {
		t.Errorf("account identity = %s, want the existing %s", account.KratosIdentityID, identityID)
	}
	if len(env.logs.events(t, "bootstrap_identity_reused")) != 1 {
		t.Error("bootstrap_identity_reused was not logged once")
	}
	env.assertNoSecretInLogs()
}

func TestAKratosConflictOnCreateReusesTheExistingIdentity(t *testing.T) {
	env := newBootstrapEnv(t)
	identityID := env.fake.addIdentity(testAdminEmail)
	env.fake.hideFirstFind = true
	env.mustBuild(nil)

	if env.fake.count(http.MethodPost) != 1 {
		t.Fatalf("Kratos saw %d creates, want the one that met the 409", env.fake.count(http.MethodPost))
	}
	for _, req := range env.fake.received() {
		isIdentityChange := req.Method == http.MethodPut || req.Method == http.MethodPatch || req.Method == http.MethodDelete
		if isIdentityChange {
			t.Errorf("unexpected %s %s: the password must never change", req.Method, req.Path)
		}
	}
	if env.account(testAdminEmail).KratosIdentityID != identityID {
		t.Error("the account does not follow the existing identity")
	}
	if len(env.logs.events(t, "bootstrap_identity_reused")) != 1 {
		t.Error("bootstrap_identity_reused was not logged once")
	}
}

func TestKratosDownPastTheTimeoutFailsBuildAndReleasesTheLock(t *testing.T) {
	env := newBootstrapEnv(t)
	env.fake.failures = 1 << 30
	started := time.Now()
	_, err := env.build(map[string]string{"GATBASE_BOOTSTRAP_TIMEOUT": "5s"})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Build succeeded with Kratos down")
	}
	if elapsed < 5*time.Second {
		t.Errorf("Build gave up after %s, before the 5s timeout", elapsed)
	}
	if env.fake.count(http.MethodGet) < 3 {
		t.Errorf("Kratos saw %d lookups, want retries", env.fake.count(http.MethodGet))
	}
	if env.count("ten_tenants") != 0 || env.count("iam_accounts") != 0 {
		t.Error("a failed bootstrap left rows behind")
	}
	// The failed run must have released its lock: no session holds it.
	if env.advisoryLockHolders() != 0 {
		t.Error("the bootstrap lock is still held after a failed run")
	}
	env.assertNoSecretInLogs()
}

func TestALockHeldByAnotherSessionFailsBuildAtTheTimeout(t *testing.T) {
	env := newBootstrapEnv(t)
	holder, err := env.pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, err = holder.Exec(context.Background(), "SELECT pg_advisory_lock($1)", bootstrapLockKey)
	if err != nil {
		holder.Release()
		t.Fatalf("hold lock: %v", err)
	}
	defer func() {
		_, _ = holder.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", bootstrapLockKey)
		holder.Release()
	}()

	started := time.Now()
	_, err = env.build(map[string]string{"GATBASE_BOOTSTRAP_TIMEOUT": "5s"})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Build succeeded while another session held the bootstrap lock")
	}
	if elapsed < 4*time.Second || elapsed > 15*time.Second {
		t.Errorf("Build gave up after %s, want about the 5s timeout", elapsed)
	}
	if env.count("ten_tenants") != 0 || len(env.fake.received()) != 0 {
		t.Error("bootstrap ran without holding the lock")
	}
}

func TestKratosRecoveringWithinTheTimeoutLetsBootstrapFinish(t *testing.T) {
	env := newBootstrapEnv(t)
	env.fake.failures = 2
	env.mustBuild(map[string]string{"GATBASE_BOOTSTRAP_TIMEOUT": "60s"})

	if env.count("ten_tenants") != 1 || env.count("ten_memberships") != 1 {
		t.Fatal("bootstrap did not finish after Kratos recovered")
	}
}

func TestAPartialBootstrapConfigListsEveryProblemInOneError(t *testing.T) {
	env := newBootstrapEnv(t)
	_, err := env.build(map[string]string{
		"GATBASE_BOOTSTRAP_ADMIN_EMAIL":    "",
		"GATBASE_BOOTSTRAP_ADMIN_PASSWORD": "",
	})
	if err == nil {
		t.Fatal("Build accepted a partial bootstrap configuration")
	}
	for _, key := range []string{"GATBASE_BOOTSTRAP_ADMIN_EMAIL", "GATBASE_BOOTSTRAP_ADMIN_PASSWORD"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not name %s: %v", key, err)
		}
	}
	if env.count("ten_tenants") != 0 || len(env.fake.received()) != 0 {
		t.Error("a rejected configuration ran the bootstrap")
	}
}

func TestBootstrapDisabledCreatesNothing(t *testing.T) {
	env := newBootstrapEnv(t)
	env.mustBuild(map[string]string{
		"GATBASE_BOOTSTRAP_TENANT_SLUG":    "",
		"GATBASE_BOOTSTRAP_ADMIN_EMAIL":    "",
		"GATBASE_BOOTSTRAP_ADMIN_PASSWORD": "",
	})
	if env.count("ten_tenants") != 0 || env.count("iam_accounts") != 0 || env.count("ten_memberships") != 0 {
		t.Error("a disabled bootstrap created rows")
	}
	if len(env.fake.received()) != 0 {
		t.Error("a disabled bootstrap called Kratos")
	}
}

func TestAnExistingTenantSkipsEvenWithDifferentVariables(t *testing.T) {
	env := newBootstrapEnv(t)
	env.seedTenant("existing", "Existing")
	env.mustBuild(nil)

	if env.count("ten_tenants") != 1 || env.count("iam_accounts") != 0 {
		t.Error("the bootstrap ran although a tenant exists")
	}
	if len(env.fake.received()) != 0 {
		t.Error("the bootstrap called Kratos although a tenant exists")
	}
}

func TestListedOperatorEmailsGrantExistingAccountsOnly(t *testing.T) {
	env := newBootstrapEnv(t)
	ctx := context.Background()
	env.seedTenant("existing", "Existing")
	for _, email := range []string{testJaneEmail, "john.roe@example.com", "kept@example.com"} {
		env.seedAccount(randomUUID(t), email)
	}
	_, err := env.pool.Exec(ctx, "UPDATE iam_accounts SET is_platform_operator = true WHERE email = 'kept@example.com'")
	if err != nil {
		t.Fatalf("seed flag: %v", err)
	}

	env.mustBuild(map[string]string{"GATBASE_PLATFORM_OPERATOR_EMAILS": "Jane.Doe@example.com,absent@example.com"})

	if !env.account(testJaneEmail).IsPlatformOperator {
		t.Error("the listed account was not granted")
	}
	if env.account("john.roe@example.com").IsPlatformOperator {
		t.Error("an unlisted account was granted")
	}
	if !env.account("kept@example.com").IsPlatformOperator {
		t.Error("an account that left the list lost the flag")
	}
	if env.count("iam_accounts") != 3 {
		t.Error("the grant created an account")
	}
	applied := env.logs.events(t, "platform_operators_applied")
	if len(applied) != 1 || applied[0]["count"] != float64(1) {
		t.Errorf("platform_operators_applied = %v, want count 1", applied)
	}
}

func TestOperatorGrantsApplyWhenBootstrapIsDisabledAndNeverRevoke(t *testing.T) {
	env := newBootstrapEnv(t)
	env.seedAccount(randomUUID(t), testJaneEmail)
	disabled := map[string]string{
		"GATBASE_BOOTSTRAP_TENANT_SLUG":    "",
		"GATBASE_BOOTSTRAP_ADMIN_EMAIL":    "",
		"GATBASE_BOOTSTRAP_ADMIN_PASSWORD": "",
	}
	disabled["GATBASE_PLATFORM_OPERATOR_EMAILS"] = testJaneEmail
	env.mustBuild(disabled)
	if !env.account(testJaneEmail).IsPlatformOperator {
		t.Fatal("the listed account was not granted")
	}
	// The email leaves the list: the flag stays.
	disabled["GATBASE_PLATFORM_OPERATOR_EMAILS"] = ""
	env.mustBuild(disabled)
	if !env.account(testJaneEmail).IsPlatformOperator {
		t.Error("removing the email from the list revoked the flag")
	}
}

func TestAKratosRejectionFailsBuildWithoutRetrying(t *testing.T) {
	env := newBootstrapEnv(t)
	env.fake.rejectStatus = http.StatusBadRequest
	started := time.Now()
	_, err := env.build(map[string]string{"GATBASE_BOOTSTRAP_TIMEOUT": "60s"})

	if err == nil {
		t.Fatal("Build succeeded although Kratos rejected the request")
	}
	if time.Since(started) >= 30*time.Second {
		t.Error("a non-retryable Kratos answer was retried until the timeout")
	}
	if env.fake.count(http.MethodPost) != 1 {
		t.Errorf("Kratos saw %d creates, want 1", env.fake.count(http.MethodPost))
	}
	if env.count("ten_tenants") != 0 {
		t.Error("a failed bootstrap left rows behind")
	}
}
