package server

import (
	"crypto/subtle"
	"mime"
	"net/http"
	"strings"
	"time"
)

// contentSecurityPolicy lets the UI load only its own scripts, styles and
// connections, and never be framed.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// maxBody caps request bodies. File writes get more room for the JSON
// escaping of content that may itself be up to maxFileSize.
const (
	maxBody      = 1 << 20
	maxWriteBody = 4 << 20
)

// withSecurityHeaders sets the headers every response carries — the UI, API
// errors and 404s alike.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// withHostCheck refuses requests addressed to a host this server is not.
// A DNS-rebinding page reaches the loopback port under its own name, so
// checking Host is what tells it apart from the UI.
func (s *Server) withHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hosts.allows(r.Host) {
			writeError(w, newError(codeMisdirected, "host %q is not served here; start loom serve with --allowed-host to accept it", r.Host))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// guard applies the API's request checks: Origin, JSON content type on
// mutating requests, the body cap and, when auth is set, the session token.
func (s *Server) guard(auth bool, bodyLimit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !s.sameOrigin(origin, r.Host) {
			writeError(w, errForbidden("cross-origin requests are not accepted"))
			return
		}
		if isMutating(r.Method) && !isJSON(r.Header.Get("Content-Type")) {
			// A form post needs no CORS preflight; demanding JSON forces
			// one, and the server never grants it.
			writeError(w, errForbidden("%s requests must have Content-Type: application/json", r.Method))
			return
		}
		if auth && !s.authenticated(r) {
			writeError(w, newError(codeUnauthorized, "missing or wrong session token"))
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sameOrigin(origin, host string) bool {
	scheme := "http://"
	if s.cfg.TLS {
		scheme = "https://"
	}
	return strings.EqualFold(origin, scheme+host)
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}

// authenticated reports whether r carries the token, as a bearer header or
// this server's session cookie. Both comparisons run in constant time.
func (s *Server) authenticated(r *http.Request) bool {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, tok, ok := strings.Cut(h, " ")
		return ok && strings.EqualFold(scheme, "Bearer") && s.tokenMatches(strings.TrimSpace(tok))
	}
	c, err := r.Cookie(s.cookieName)
	return err == nil && s.tokenMatches(c.Value)
}

func (s *Server) tokenMatches(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.token)) == 1
}

// handleSessionCreate exchanges the token for the session cookie. The cookie
// name carries the port because cookies ignore ports: another server on
// 127.0.0.1 would otherwise receive this one's session.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeBody(r, &body); err != nil {
		return err
	}
	if !s.tokenMatches(body.Token) {
		return newError(codeUnauthorized, "wrong session token")
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.TLS,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.TLS,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// statusRecorder captures the status for the access log. Unwrap lets
// http.ResponseController reach the connection's Flush and write deadline.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	if sr.status == 0 {
		sr.status = code
	}
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if sr.status == 0 {
		sr.status = http.StatusOK
	}
	return sr.ResponseWriter.Write(b)
}

func (sr *statusRecorder) Unwrap() http.ResponseWriter { return sr.ResponseWriter }

// withAccessLog logs each request's method, path, status and duration. The
// query string and headers are left out on purpose: they are where a token
// would be if a client put one in the wrong place.
func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		s.logger.Info("http", "method", r.Method, "path", r.URL.Path, "status", status,
			"duration", time.Since(start).Round(time.Millisecond).String())
	})
}
