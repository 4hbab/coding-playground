// Package handler contains HTTP request handlers for the playground application.
//
// WHAT IS A HANDLER?
// In Go, an HTTP handler is anything that implements the http.Handler interface:
//
//	type Handler interface {
//	    ServeHTTP(ResponseWriter, *Request)
//	}
//
// Or more commonly, we use http.HandlerFunc — a function with the right signature
// that automatically satisfies the Handler interface. Chi's router accepts these directly.
//
// HANDLER RESPONSIBILITIES:
// 1. Parse the incoming HTTP request (query params, body, headers)
// 2. Call business logic (or in our case, render templates)
// 3. Write the HTTP response (status code, headers, body)
//
// Handlers should NOT contain business logic — they are the "glue" between HTTP and your app.
package handler

import (
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
)

// PlaygroundHandler manages the main playground page.
// It holds parsed templates so we don't re-parse them on every request.
//
// WHY A STRUCT?
// By using a struct, we can:
// 1. Parse templates once at startup (expensive) and reuse them (cheap)
// 2. Inject dependencies (logger, config) without global variables
// 3. Group related handlers together
type PlaygroundHandler struct {
	templates *template.Template
	logger    *slog.Logger
}

// NewPlaygroundHandler creates a new PlaygroundHandler and parses the HTML templates.
//
// TEMPLATE PARSING:
// template.ParseFiles() reads HTML files and compiles them into an internal tree structure.
// We parse both "base.html" and "playground.html" together so they can reference each other:
//   - base.html defines the overall page structure with {{template "content" .}} placeholder
//   - playground.html defines {{define "content"}}...{{end}} to fill that placeholder
//
// This is Go's template composition model — similar to "extends" in Jinja2 or "layouts" in Rails.
func NewPlaygroundHandler(templateDir string, logger *slog.Logger) (*PlaygroundHandler, error) {
	// filepath.Join handles OS-specific path separators (\ on Windows, / on Linux)
	tmpl, err := template.ParseFiles(
		filepath.Join(templateDir, "base.html"),
		filepath.Join(templateDir, "playground.html"),
	)
	if err != nil {
		return nil, err
	}

	return &PlaygroundHandler{
		templates: tmpl,
		logger:    logger,
	}, nil
}

// HandlePlayground serves the main playground page.
//
// HTTP FLOW:
// 1. Browser sends GET / request
// 2. Chi router matches "/" and calls this handler
// 3. We execute the "base" template, which pulls in "content" from playground.html
// 4. The rendered HTML is written to http.ResponseWriter and sent back to the browser
func (h *PlaygroundHandler) HandlePlayground(w http.ResponseWriter, r *http.Request) {
	// Data we pass to the template (currently empty, but extensible)
	data := map[string]interface{}{
		"Title": "PyPlayground — Python Coding Playground",
	}

	// Set content type header BEFORE writing the body
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Execute the "base" template with our data
	// If template execution fails, log the error and send a 500 response
	if err := h.templates.ExecuteTemplate(w, "base", data); err != nil {
		h.logger.Error("failed to render template",
			slog.String("error", err.Error()),
		)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
