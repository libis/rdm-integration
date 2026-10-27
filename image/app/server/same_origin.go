// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// sameOriginOnly rejects browser API requests whose Origin does not match the
// configured public Dataverse origin. Non-API requests are left alone because
// Dataverse launches the integration UI through an external-tool navigation.
func sameOriginOnly(configuredURL string, next http.Handler) http.Handler {
	allowedOrigin, allowed := normalizedOrigin(configuredURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protectedPath := strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/quit"
		if protectedPath && !isSameOriginRequest(r, allowedOrigin, allowed) {
			http.Error(w, "403 - same-origin request required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSameOriginRequest(r *http.Request, allowedOrigin string, allowed bool) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		requestOrigin, valid := normalizedOrigin(origin)
		return allowed && valid && requestOrigin == allowedOrigin
	}

	// Modern browsers send Fetch Metadata even when Origin is omitted on a
	// request. Reject cross-origin and same-site requests, since sibling hosts
	// are still different origins. Requests without browser metadata remain
	// usable by command-line clients and older browsers.
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "cross-site", "same-site":
		return false
	default:
		return true
	}
}

func normalizedOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	// Support backend configs that specify a bare host:port.
	if parsed.Host == "" && !strings.Contains(raw, "://") {
		parsed, err = url.Parse("http://" + strings.TrimLeft(raw, "/"))
		if err != nil {
			return "", false
		}
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return "", false
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port), true
}
