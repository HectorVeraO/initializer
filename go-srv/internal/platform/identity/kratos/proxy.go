package kratos

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	proxyPrefix  = "/kratos"
	proxyTimeout = 5 * time.Second
	csrfCookie   = "XSRF-TOKEN"
)

// allowlist maps a method to the Kratos paths the proxy forwards.
var allowlist = map[string]map[string]bool{
	http.MethodGet: {
		"/self-service/login/browser": true,
		"/self-service/login/flows":   true,
		"/self-service/errors":        true,
	},
	http.MethodPost: {
		"/self-service/login": true,
	},
}

// credentialHeaders carry a BFF credential and never reach Kratos.
var credentialHeaders = []string{"Authorization", "X-API-Key", "X-XSRF-TOKEN", "X-Active-Tenant-Id"}

// NewProxy returns the allowlisted /kratos/ reverse proxy. It forwards to
// publicURL, removes the BFF cookies named in bffCookieNames (plus
// XSRF-TOKEN) and the credential headers, and sets X-Forwarded-Proto and
// X-Forwarded-Host from publicBase. Anything outside the allowlist answers a
// plain 404 without reaching Kratos; a transport error answers a plain 502.
func NewProxy(publicURL *url.URL, publicBase *url.URL, bffCookieNames []string, opts ...Option) http.Handler {
	var options clientOptions
	for _, apply := range opts {
		apply(&options)
	}
	dropCookies := map[string]bool{csrfCookie: true}
	for _, name := range bffCookieNames {
		dropCookies[name] = true
	}
	var transport http.RoundTripper = &http.Transport{
		DialContext:           (&net.Dialer{Timeout: proxyTimeout}).DialContext,
		ResponseHeaderTimeout: proxyTimeout,
		TLSHandshakeTimeout:   proxyTimeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	if options.wrap != nil {
		transport = options.wrap(transport)
	}
	forwardedProto := publicBase.Scheme
	forwardedHost := publicBase.Host
	target := *publicURL
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// The upstream path is derived from the allowlist match, and
			// RawPath is cleared so no client-chosen encoding is forwarded.
			upstreamPath := strings.TrimPrefix(pr.In.URL.Path, proxyPrefix)
			out := pr.Out
			out.URL.Scheme = target.Scheme
			out.URL.Host = target.Host
			out.URL.Path = joinPath(target.Path, upstreamPath)
			out.URL.RawPath = ""
			out.Host = target.Host
			for _, header := range credentialHeaders {
				out.Header.Del(header)
			}
			filterCookies(out, dropCookies)
			out.Header.Set("X-Forwarded-Proto", forwardedProto)
			out.Header.Set("X-Forwarded-Host", forwardedHost)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			plain(w, http.StatusBadGateway)
		},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed(r) {
			plain(w, http.StatusNotFound)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	return handler
}

// allowed reports whether r is one of the allowlisted method and path pairs.
func allowed(r *http.Request) bool {
	path, found := strings.CutPrefix(r.URL.Path, proxyPrefix)
	if !found {
		return false
	}
	return allowlist[r.Method][path]
}

// filterCookies rewrites the Cookie header without the dropped cookie names.
func filterCookies(req *http.Request, drop map[string]bool) {
	cookies := req.Cookies()
	req.Header.Del("Cookie")
	for _, cookie := range cookies {
		if drop[cookie.Name] {
			continue
		}
		req.AddCookie(cookie)
	}
}

func plain(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(http.StatusText(status) + "\n"))
}

// joinPath joins an upstream base path and a request path under a leading
// slash (url.JoinPath drops it when the base path is empty).
func joinPath(basePath, requestPath string) string {
	joined := path.Join("/", basePath, requestPath)
	return joined
}
