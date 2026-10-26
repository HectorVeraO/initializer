// Package kratos holds the Ory Kratos clients of core-auth (public whoami and
// logout, admin identity lookup and creation) and the allowlisted login
// reverse proxy. It uses the standard library only.
package kratos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var (
	// ErrNoSession means Kratos holds no active session for the cookie.
	ErrNoSession = errors.New("kratos: no active session")
	// ErrUnavailable means Kratos could not answer: transport error, timeout,
	// 5xx or an unreadable answer.
	ErrUnavailable = errors.New("kratos: unavailable")
	// ErrIdentityExists means an identity with the same identifier exists.
	ErrIdentityExists = errors.New("kratos: identity exists")
)

// maxBodyBytes bounds every Kratos response body read.
const maxBodyBytes = 1 << 20

// Option configures a Kratos client.
type Option func(*clientOptions)

type clientOptions struct {
	transport http.RoundTripper
	wrap      func(http.RoundTripper) http.RoundTripper
}

// WithTransportWrapper wraps the round tripper the proxy uses, which keeps
// its bounded transport inside the wrapper, for example
// Telemetry.Transport so the Kratos hop carries the W3C traceparent. A nil
// wrapper keeps the default.
func WithTransportWrapper(wrap func(http.RoundTripper) http.RoundTripper) Option {
	return func(options *clientOptions) {
		options.wrap = wrap
	}
}

// WithTransport sets the round tripper of the client, for example
// Telemetry.Transport(nil) so calls carry the W3C traceparent. A nil
// transport keeps the default.
func WithTransport(transport http.RoundTripper) Option {
	return func(options *clientOptions) {
		options.transport = transport
	}
}

// newHTTPClient returns a client that never follows redirects.
func newHTTPClient(timeout time.Duration, opts []Option) *http.Client {
	var options clientOptions
	for _, apply := range opts {
		apply(&options)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: options.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// do sends req and returns the status and the bounded body. Transport errors
// map to ErrUnavailable without the request URL, which may carry an email.
func do(client *http.Client, req *http.Request) (int, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		cause := err
		if errors.As(err, &urlErr) {
			cause = urlErr.Err
		}
		return 0, nil, fmt.Errorf("%w: %w", ErrUnavailable, cause)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: reading body: %w", ErrUnavailable, err)
	}
	return resp.StatusCode, body, nil
}

// endpoint returns base joined with path; base is never modified.
func endpoint(base *url.URL, path string) *url.URL {
	joined := *base
	joined.RawQuery = ""
	joined.Fragment = ""
	return joined.JoinPath(path)
}

func newRequest(ctx context.Context, method string, target *url.URL, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %w", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func decode(body []byte, into any) error {
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%w: undecodable answer", ErrUnavailable)
	}
	return nil
}
