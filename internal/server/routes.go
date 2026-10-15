package server

import "net/http"

// routes builds the handler: security headers and the Host check around
// everything, the API's own checks around every API route.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	api := func(pattern string, h handlerFunc) {
		mux.Handle(pattern, s.guard(true, maxBody, s.api(h)))
	}

	// The session exchange is the one API call made without the token.
	mux.Handle("POST /api/v1/session", s.guard(false, maxBody, s.api(s.handleSessionCreate)))
	mux.Handle("DELETE /api/v1/session", s.guard(false, maxBody, s.api(s.handleSessionDelete)))

	api("GET /api/v1/info", s.handleInfo)
	api("GET /api/v1/modules", s.handleModulesList)
	api("POST /api/v1/modules", s.handleModuleCreate)

	api("POST /api/v1/inspect", s.handleInspect)
	api("POST /api/v1/validate", s.handleValidate)
	api("POST /api/v1/params/check", s.handleParamsCheck)
	api("POST /api/v1/yaml/parse", s.handleYAMLParse)
	api("POST /api/v1/yaml/format", s.handleYAMLFormat)

	api("GET /api/v1/files", s.handleFilesGet)
	mux.Handle("PUT /api/v1/files", s.guard(true, maxWriteBody, s.api(s.handleFilesPut)))
	api("POST /api/v1/files", s.handleFilesCreate)
	api("DELETE /api/v1/files", s.handleFilesDelete)
	api("POST /api/v1/files/move", s.handleFilesMove)

	api("POST /api/v1/jobs", s.handleJobCreate)
	api("GET /api/v1/jobs", s.handleJobList)
	api("GET /api/v1/jobs/{id}", s.handleJobGet)
	api("DELETE /api/v1/jobs/{id}", s.handleJobDelete)
	api("GET /api/v1/jobs/{id}/events", s.handleJobEvents)
	api("GET /api/v1/jobs/{id}/diff", s.handleJobDiff)
	api("GET /api/v1/jobs/{id}/log.txt", s.handleJobLog)
	api("POST /api/v1/jobs/{id}/cancel", s.handleJobCancel)
	api("POST /api/v1/jobs/{id}/rerun", s.handleJobRerun)

	api("GET /api/v1/presets", s.handlePresetList)
	api("GET /api/v1/presets/{name}", s.handlePresetGet)
	api("PUT /api/v1/presets/{name}", s.handlePresetPut)
	api("DELETE /api/v1/presets/{name}", s.handlePresetDelete)

	api("POST /api/v1/cli", s.handleCLI)
	api("GET /api/v1/docs", s.handleDocs)
	api("GET /api/v1/docs/{topic...}", s.handleDocTopic)

	// Anything else under /api is an unknown endpoint — or a known one with
	// the wrong method — and gets the JSON error envelope, not the UI.
	mux.Handle("/api/", s.guard(true, maxBody, s.api(func(w http.ResponseWriter, r *http.Request) error {
		return errNotFound("no endpoint %s %s", r.Method, r.URL.Path)
	})))
	mux.HandleFunc("/", s.handleStatic)

	return s.withAccessLog(withSecurityHeaders(s.withHostCheck(mux)))
}
