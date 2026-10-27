package config_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/config"
)

func reader(m map[string]string) *config.Reader {
	return config.NewReader(config.MapSource(m))
}

func TestDefaultsApplyWhenUnsetOrBlank(t *testing.T) {
	r := reader(map[string]string{"BLANK": "   "})
	if r.String("MISSING", "fb") != "fb" || r.String("BLANK", "fb") != "fb" {
		t.Fatal("String fallback")
	}
	if r.Int("MISSING", 7, 1, 10) != 7 || r.Duration("MISSING", time.Second, 0, time.Minute) != time.Second || !r.Bool("MISSING", true) {
		t.Fatal("typed fallback")
	}
	if r.List("MISSING") != nil {
		t.Fatal("List of unset is not nil")
	}
	if r.Err() != nil {
		t.Fatalf("err = %v", r.Err())
	}
}

func TestValidValuesAreRead(t *testing.T) {
	r := reader(map[string]string{
		"S": " :10100 ", "I": "25", "D": "30s", "B": "true", "L": " a, b ,,c ,", "U": "http://localhost:10120",
	})
	if r.String("S", "") != ":10100" || r.Int("I", 1, 1, 200) != 25 || r.Duration("D", 0, time.Second, time.Minute) != 30*time.Second || !r.Bool("B", false) {
		t.Fatal("scalars")
	}
	if got := strings.Join(r.List("L"), "|"); got != "a|b|c" {
		t.Fatalf("list = %q", got)
	}
	if u := r.URL("U", "http://x"); u == nil || u.Host != "localhost:10120" {
		t.Fatalf("url = %v", u)
	}
	if r.RequiredString("S") != ":10100" || r.Err() != nil {
		t.Fatalf("err = %v", r.Err())
	}
}

func TestThreeBadKeysReportedInOneError(t *testing.T) {
	r := reader(map[string]string{"PORT": "abc", "TIMEOUT": "forever", "CONNS": "5000", "FLAG": "maybe"})
	if r.Int("PORT", 9, 1, 100) != 9 {
		t.Fatal("fallback not returned")
	}
	r.Duration("TIMEOUT", time.Second, time.Second, time.Minute)
	r.Int("CONNS", 10, 1, 200)
	r.Bool("FLAG", false)
	r.RequiredString("ABSENT")

	text := r.Err().Error()
	for _, key := range []string{"PORT", "TIMEOUT", "CONNS", "FLAG", "ABSENT"} {
		if !strings.Contains(text, key) {
			t.Errorf("error does not name %s: %s", key, text)
		}
	}
	for _, value := range []string{"abc", "forever", "5000", "maybe"} {
		if strings.Contains(text, value) {
			t.Errorf("error leaks value %q: %s", value, text)
		}
	}
}

func TestRangeBoundariesAreInclusive(t *testing.T) {
	r := reader(map[string]string{"LOW": "1", "HIGH": "200", "UNDER": "0", "OVER": "201", "DLOW": "1s", "DUNDER": "999ms"})
	r.Int("LOW", 0, 1, 200)
	r.Int("HIGH", 0, 1, 200)
	r.Duration("DLOW", 0, time.Second, time.Minute)
	if r.Err() != nil {
		t.Fatalf("boundaries rejected: %v", r.Err())
	}
	r.Int("UNDER", 0, 1, 200)
	r.Int("OVER", 0, 1, 200)
	r.Duration("DUNDER", 0, time.Second, time.Minute)
	text := r.Err().Error()
	for _, key := range []string{"UNDER", "OVER", "DUNDER"} {
		if !strings.Contains(text, key) {
			t.Errorf("error does not name %s: %s", key, text)
		}
	}
	for _, key := range []string{"LOW", "HIGH"} {
		if strings.Contains(text, key) {
			t.Errorf("error names accepted key %s: %s", key, text)
		}
	}
}

func TestURLRules(t *testing.T) {
	bad := []string{"localhost:10120", "ftp://x.example.com", "http://", "/relative", "http://x.example.com?a=1",
		"http://x.example.com#f", "http://x.example.com/#", "http://user:change-me@x.example.com", "not a url", "javascript:alert(1)"}
	for _, v := range bad {
		r := reader(map[string]string{"U": v})
		if u := r.URL("U", "http://ok.example.com"); u != nil || r.Err() == nil {
			t.Errorf("%q accepted", v)
		} else if strings.Contains(r.Err().Error(), "change-me") {
			t.Errorf("credentials leaked: %v", r.Err())
		}
	}
	good := reader(map[string]string{"U": "https://app.example.com/base/"})
	if u := good.URL("U", "http://ok.example.com"); u == nil || u.Path != "/base/" || good.Err() != nil {
		t.Fatalf("url = %v err %v", u, good.Err())
	}
	fb := reader(nil)
	if u := fb.URL("U", "http://localhost:10120"); u == nil || fb.Err() != nil {
		t.Fatalf("fallback url = %v err %v", u, fb.Err())
	}
}

func TestSecretNeverAppearsInErrFmtOrSlog(t *testing.T) {
	const value = "change-me-super-value"
	r := reader(map[string]string{"DB": value, "BAD": "x"})
	secret := r.Secret("DB")
	r.Int("BAD", 1, 1, 2)
	if secret.Reveal() != value {
		t.Fatal("Reveal")
	}
	if strings.Contains(r.Err().Error(), value) {
		t.Fatal("secret in Err")
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	logger.Info("config_loaded", slog.Any("database_url", secret), "inline", secret)
	asJSON, err := json.Marshal(map[string]config.Secret{"k": secret})
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{
		fmt.Sprint(secret), fmt.Sprintf("%v", secret), fmt.Sprintf("%+v", secret), fmt.Sprintf("%#v", secret),
		fmt.Sprintf("%s", secret), fmt.Sprintf("%v", &secret), fmt.Sprintf("%+v", struct{ S config.Secret }{secret}),
		logs.String(), string(asJSON),
	}
	for i, out := range outputs {
		if strings.Contains(out, value) {
			t.Errorf("output %d leaks the secret: %s", i, out)
		}
	}
	if secret.String() != "[redacted]" || secret.LogValue().String() != "[redacted]" {
		t.Fatal("redaction text")
	}
}

func TestMissingRequiredSecretNamesKeyOnly(t *testing.T) {
	r := reader(nil)
	if r.Secret("GATBASE_DATABASE_URL").Reveal() != "" {
		t.Fatal("value from nowhere")
	}
	if !strings.Contains(r.Err().Error(), "GATBASE_DATABASE_URL") {
		t.Fatalf("err = %v", r.Err())
	}
}

func TestOptionalSecretUnsetRecordsNoProblem(t *testing.T) {
	r := reader(map[string]string{"SET": "change-me"})
	if s, ok := r.OptionalSecret("UNSET"); ok || s.Reveal() != "" {
		t.Fatal("unset reported present")
	}
	if s, ok := r.OptionalSecret("SET"); !ok || s.Reveal() != "change-me" {
		t.Fatal("set reported absent")
	}
	if r.Err() != nil {
		t.Fatalf("err = %v", r.Err())
	}
}

func TestInvalidRecordsKeyAndRuleOnly(t *testing.T) {
	r := reader(map[string]string{"SLUG": "Not A Slug!"})
	r.Invalid("SLUG", "must be lowercase letters and hyphens")
	text := r.Err().Error()
	if !strings.Contains(text, "SLUG") || !strings.Contains(text, "must be lowercase letters and hyphens") || strings.Contains(text, "Not A Slug") {
		t.Fatalf("err = %q", text)
	}
}

func TestNewReaderRejectsNilSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	config.NewReader(nil)
}

func TestEnvSourceReadsProcessEnvironment(t *testing.T) {
	t.Setenv("GATBASE_TEST_PROBE", "value-1")
	if v, ok := config.EnvSource()("GATBASE_TEST_PROBE"); !ok || v != "value-1" {
		t.Fatalf("got %q %v", v, ok)
	}
}
