package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// raw sends a request with exactly the given headers, no token added.
func (e *testEnv) raw(method, path, body string, headers map[string]string) (*http.Response, string) {
	e.t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.base+path, r)
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func TestServe_SV2_TokenRequired(t *testing.T) {
	e := newEnv(t)
	u, _ := url.Parse(e.base)
	good := "Bearer " + e.srv.Token()
	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"no token", nil, http.StatusUnauthorized},
		{"wrong bearer", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"token without scheme", map[string]string{"Authorization": e.srv.Token()}, http.StatusUnauthorized},
		{"token prefix", map[string]string{"Authorization": "Bearer " + e.srv.Token()[:10]}, http.StatusUnauthorized},
		{"bearer", map[string]string{"Authorization": good}, http.StatusOK},
		{"cookie", map[string]string{"Cookie": "loom_session_" + u.Port() + "=" + e.srv.Token()}, http.StatusOK},
		{"cookie of another port", map[string]string{"Cookie": "loom_session_1=" + e.srv.Token()}, http.StatusUnauthorized},
		{"wrong cookie", map[string]string{"Cookie": "loom_session_" + u.Port() + "=nope"}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := e.raw(http.MethodGet, "/api/v1/info", "", tc.headers)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, tc.want, body)
			}
			if tc.want == http.StatusUnauthorized && !strings.Contains(body, `"code":"unauthorized"`) {
				t.Errorf("body %s lacks the unauthorized code", body)
			}
		})
	}
	// Every API route is behind the token, including unknown ones.
	for _, p := range []string{"/api/v1/modules", "/api/v1/jobs", "/api/v1/docs", "/api/v1/nope"} {
		if resp, _ := e.raw(http.MethodGet, p, "", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without a token: %d", p, resp.StatusCode)
		}
	}
	// 256 bits of randomness, hex-encoded.
	if len(e.srv.Token()) != 64 {
		t.Errorf("token %q is not 256 bits of hex", e.srv.Token())
	}
	other := newEnv(t)
	if other.srv.Token() == e.srv.Token() {
		t.Error("two servers share a token")
	}
}

func TestServe_SV3_SessionCookie(t *testing.T) {
	e := newEnv(t)
	u, _ := url.Parse(e.base)
	json := map[string]string{"Content-Type": "application/json"}

	resp, _ := e.raw(http.MethodPost, "/api/v1/session", `{"token":"wrong"}`, json)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatal("a wrong token got a cookie")
	}

	resp, _ = e.raw(http.MethodPost, "/api/v1/session", `{"token":"`+e.srv.Token()+`"}`, json)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d, want 204", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	c := cookies[0]
	if c.Name != "loom_session_"+u.Port() || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Errorf("cookie %+v is not HttpOnly, SameSite=Strict, Path=/, port-named", c)
	}
	if resp, _ := e.raw(http.MethodGet, "/api/v1/info", "", map[string]string{"Cookie": c.Name + "=" + c.Value}); resp.StatusCode != http.StatusOK {
		t.Errorf("the session cookie is not accepted: %d", resp.StatusCode)
	}

	resp, _ = e.raw(http.MethodDelete, "/api/v1/session", "", json)
	if resp.StatusCode != http.StatusNoContent || len(resp.Cookies()) != 1 || resp.Cookies()[0].MaxAge >= 0 {
		t.Errorf("DELETE /session did not clear the cookie: %d %v", resp.StatusCode, resp.Cookies())
	}

	// The token reaches the browser only in the fragment, which is never
	// sent to a server.
	pu, err := url.Parse(e.srv.URL())
	if err != nil {
		t.Fatal(err)
	}
	if pu.Fragment != "token="+e.srv.Token() || strings.Contains(pu.Path+pu.RawQuery, e.srv.Token()) {
		t.Errorf("startup URL %q must carry the token in its fragment only", e.srv.URL())
	}
}

func TestServe_SV4_HostCheck(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AllowedHosts = []string{"loom.example", "box.lan:9999"} })
	u, _ := url.Parse(e.base)
	auth := "Bearer " + e.srv.Token()
	cases := []struct {
		host string
		want int
	}{
		{u.Host, http.StatusOK},
		{"localhost:" + u.Port(), http.StatusOK},
		{"[::1]:" + u.Port(), http.StatusOK},
		{"loom.example:" + u.Port(), http.StatusOK},
		{"loom.example", http.StatusOK},
		{"box.lan:9999", http.StatusOK},
		{"box.lan:" + u.Port(), http.StatusMisdirectedRequest},
		{"evil.example:" + u.Port(), http.StatusMisdirectedRequest},
		{"127.0.0.1:1", http.StatusMisdirectedRequest},
	}
	for _, tc := range cases {
		resp, body := e.raw(http.MethodGet, "/api/v1/info", "", map[string]string{"Host": tc.host, "Authorization": auth})
		if resp.StatusCode != tc.want {
			t.Errorf("Host %s: status %d, want %d: %s", tc.host, resp.StatusCode, tc.want, body)
		}
		if tc.want == http.StatusMisdirectedRequest && !strings.Contains(body, `"code":"misdirected"`) {
			t.Errorf("Host %s: body %s lacks the misdirected code", tc.host, body)
		}
	}
	// The UI itself is refused to a rebinding page too.
	if resp, _ := e.raw(http.MethodGet, "/", "", map[string]string{"Host": "evil.example:" + u.Port()}); resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("static UI under a foreign Host: %d", resp.StatusCode)
	}
}

func TestServe_SV5_OriginAndContentType(t *testing.T) {
	e := newEnv(t)
	auth := "Bearer " + e.srv.Token()
	body := `{"text":"a: 1\n"}`
	cases := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		want    int
	}{
		{"same origin", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Origin": e.base, "Content-Type": "application/json", "Authorization": auth}, http.StatusOK},
		{"no origin", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Content-Type": "application/json", "Authorization": auth}, http.StatusOK},
		{"foreign origin", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json", "Authorization": auth}, http.StatusForbidden},
		{"other port origin", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Origin": "http://127.0.0.1:1", "Content-Type": "application/json", "Authorization": auth}, http.StatusForbidden},
		{"null origin", http.MethodGet, "/api/v1/info", map[string]string{"Origin": "null", "Authorization": auth}, http.StatusForbidden},
		{"foreign origin read", http.MethodGet, "/api/v1/info", map[string]string{"Origin": "http://evil.example", "Authorization": auth}, http.StatusForbidden},
		{"form post", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": auth}, http.StatusForbidden},
		{"multipart post", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Content-Type": "multipart/form-data; boundary=x", "Authorization": auth}, http.StatusForbidden},
		{"text post", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Content-Type": "text/plain", "Authorization": auth}, http.StatusForbidden},
		{"no content type", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Authorization": auth}, http.StatusForbidden},
		{"form session exchange", http.MethodPost, "/api/v1/session", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusForbidden},
		{"json with charset", http.MethodPost, "/api/v1/yaml/parse", map[string]string{"Content-Type": "application/json; charset=utf-8", "Authorization": auth}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, got := e.raw(tc.method, tc.path, body, tc.headers)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, tc.want, got)
			}
			if tc.want == http.StatusForbidden && !strings.Contains(got, `"code":"forbidden"`) {
				t.Errorf("body %s lacks the forbidden code", got)
			}
			// No CORS grant, ever.
			if resp.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Error("response grants CORS")
			}
		})
	}
}

func TestServe_SV6_SecurityHeadersEverywhere(t *testing.T) {
	e := newEnv(t)
	auth := map[string]string{"Authorization": "Bearer " + e.srv.Token()}
	cases := []struct {
		name, path string
		headers    map[string]string
		want       int
	}{
		{"ui", "/", nil, http.StatusOK},
		{"static file", "/index.html", nil, http.StatusOK},
		{"hash-routed path", "/some/where", nil, http.StatusOK},
		{"api", "/api/v1/info", auth, http.StatusOK},
		{"api 404", "/api/v1/nope", auth, http.StatusNotFound},
		{"api 401", "/api/v1/info", nil, http.StatusUnauthorized},
		{"job 404", "/api/v1/jobs/j_nope", auth, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := e.raw(http.MethodGet, tc.path, "", tc.headers)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
			h := resp.Header
			want := map[string]string{
				"Content-Security-Policy": contentSecurityPolicy,
				"X-Frame-Options":         "DENY",
				"X-Content-Type-Options":  "nosniff",
				"Referrer-Policy":         "no-referrer",
			}
			for k, v := range want {
				if h.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, h.Get(k), v)
				}
			}
		})
	}
	if !strings.Contains(contentSecurityPolicy, "frame-ancestors 'none'") || !strings.Contains(contentSecurityPolicy, "default-src 'self'") {
		t.Error("CSP lost its core directives")
	}
}

func TestServe_StaticFiles(t *testing.T) {
	e := newEnv(t)
	resp, body := e.raw(http.MethodGet, "/", "", nil)
	etag := resp.Header.Get("ETag")
	if etag == "" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("static response lacks ETag or no-cache: %v", resp.Header)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") || !strings.Contains(body, "<html") {
		t.Errorf("GET / did not serve index.html: %s", body)
	}
	resp, _ = e.raw(http.MethodGet, "/", "", map[string]string{"If-None-Match": etag})
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("revalidation with the ETag: %d, want 304", resp.StatusCode)
	}
	if resp, _ := e.raw(http.MethodPost, "/", "", nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /: %d", resp.StatusCode)
	}
}

func TestServe_BodyLimit(t *testing.T) {
	e := newEnv(t)
	big := `{"text":"` + strings.Repeat("a", maxBody) + `"}`
	if code := e.errorCode(http.MethodPost, "/api/v1/yaml/parse", big, http.StatusBadRequest); code != codeInvalid {
		t.Errorf("code %q", code)
	}
}

// The access log records method, path and status — never the query string
// or a header, where a misplaced token would be.
func TestServe_AccessLogOmitsSecrets(t *testing.T) {
	var buf bytes.Buffer
	var mu = &lockedBuffer{b: &buf}
	e := newEnv(t, func(c *Config) { c.Logger = slog.New(slog.NewTextHandler(mu, nil)) })
	e.raw(http.MethodGet, "/api/v1/info?token=querysecret", "", map[string]string{"Authorization": "Bearer " + e.srv.Token()})
	e.raw(http.MethodGet, "/api/v1/info", "", map[string]string{"Cookie": "loom_session_1=cookiesecret"})
	log := mu.String()
	if !strings.Contains(log, "/api/v1/info") {
		t.Fatalf("access log lacks the request: %s", log)
	}
	for _, secret := range []string{"querysecret", "cookiesecret", e.srv.Token(), "token="} {
		if strings.Contains(log, secret) {
			t.Errorf("access log contains %q: %s", secret, log)
		}
	}
}
