// Package server sets up the HTTP server, router, and all route definitions.
//
// SERVER ARCHITECTURE:
// This package is the "wiring" layer — it connects handlers, middleware, and routes.
// Think of it as the control centre that decides:
// - Which URL patterns map to which handler functions
// - What middleware runs on which routes
// - How the server starts and stops gracefully
//
// WHY SEPARATE FROM main.go?
// Keeping server setup in its own package makes it:
// - Testable (we can create a test server without running main)
// - Reusable (multiple entry points could use the same server config)
// - Clean (main.go stays minimal — just "start the server")
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/sakif/coding-playground/internal/handler"
	"github.com/sakif/coding-playground/internal/middleware"
)

// Config holds server configuration.
// Using a struct for config (instead of individual parameters) makes it easy to:
// - Add new config options without changing function signatures
// - Pass config around as a single value
// - Load config from files/env vars in one place
type Config struct {
	Port        int
	TemplateDir string
	StaticDir   string
}

// Server represents the HTTP server and all its dependencies.
type Server struct {
	router *chi.Mux
	config Config
	logger *slog.Logger
}

// New creates a new Server with the given config.
//
// DEPENDENCY INJECTION:
// Instead of creating dependencies inside the Server, we accept them as parameters.
// This is dependency injection — the caller "injects" what the server needs.
// Benefits:
// - Testing: pass a mock logger, different config
// - Flexibility: swap implementations without changing Server code
func New(cfg Config, logger *slog.Logger) (*Server, error) {
	s := &Server{
		router: chi.NewRouter(),
		config: cfg,
		logger: logger,
	}

	// Set up middleware and routes
	if err := s.setupRoutes(); err != nil {
		return nil, fmt.Errorf("setting up routes: %w", err)
	}

	return s, nil
}

// setupRoutes configures all middleware and route handlers.
//
// ROUTE STRUCTURE:
// GET  /              → Playground page (HTML)
// GET  /static/*      → Static files (CSS, JS, images)
// GET  /api/snippets  → List snippets (JSON)
// POST /api/snippets  → Create snippet (JSON)
// DELETE /api/snippets/{id} → Delete snippet
//
// MIDDLEWARE ORDER MATTERS:
// Middleware executes in the order it's added. Our order:
// 1. RequestID — assigns unique ID to each request (for tracing)
// 2. RealIP — extracts real client IP from proxy headers
// 3. Logger — logs each request with timing info
// 4. Recoverer — catches panics and returns 500 instead of crashing
func (s *Server) setupRoutes() error {
	// === Global Middleware ===
	// These run on EVERY request, in order

	// Chi's built-in middleware
	s.router.Use(chimiddleware.RequestID) // Adds X-Request-ID header
	s.router.Use(chimiddleware.RealIP)    // Extracts real IP from X-Forwarded-For
	s.router.Use(chimiddleware.Recoverer) // Recovers from panics, returns 500

	// Our custom logging middleware
	s.router.Use(middleware.Logger(s.logger))

	// === Static Files ===
	// http.FileServer serves files from the filesystem.
	// http.StripPrefix removes "/static/" from the URL path before looking up the file.
	// So GET /static/css/style.css → serves {StaticDir}/css/style.css
	fileServer := http.FileServer(http.Dir(s.config.StaticDir))
	s.router.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// === Page Routes ===
	playgroundHandler, err := handler.NewPlaygroundHandler(s.config.TemplateDir, s.logger)
	if err != nil {
		return fmt.Errorf("creating playground handler: %w", err)
	}
	s.router.Get("/", playgroundHandler.HandlePlayground)

	// === API Routes ===
	// chi.Route() creates a sub-router for a path prefix.
	// All routes inside share the "/api" prefix.
	snippetHandler := handler.NewSnippetHandler(s.logger)
	s.router.Route("/api", func(r chi.Router) {
		r.Get("/snippets", snippetHandler.HandleList)
		r.Post("/snippets", snippetHandler.HandleCreate)
		r.Delete("/snippets/{id}", snippetHandler.HandleDelete)
	})

	return nil
}

// Start starts the HTTP server and handles graceful shutdown.
//
// GRACEFUL SHUTDOWN:
// When you press Ctrl+C (or the OS sends SIGTERM), we don't want to immediately kill
// the server — that could interrupt in-flight requests. Instead:
//
// 1. We listen for OS signals (SIGINT, SIGTERM) in a goroutine
// 2. When a signal arrives, we call server.Shutdown() with a timeout
// 3. Shutdown() stops accepting new connections and waits for existing ones to finish
// 4. If they don't finish within the timeout, we force-close
//
// GOROUTINES:
// A goroutine is a lightweight thread managed by Go's runtime. We create one with `go func()`.
// go func() { ... }() — this creates and immediately starts a goroutine.
// They're incredibly cheap (a few KB of stack) compared to OS threads (1+ MB).
//
// CHANNELS:
// `make(chan os.Signal, 1)` creates a buffered channel that can hold one signal.
// Channels are Go's primary mechanism for communication between goroutines.
// `signal.Notify(quit, ...)` tells Go to send OS signals to our channel.
// `<-quit` blocks until a value is received from the channel.
func (s *Server) Start() error {
	// Create the HTTP server with sensible timeouts
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to receive OS signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Channel to receive server errors
	serverErrors := make(chan error, 1)

	// Start the server in a goroutine (so it doesn't block)
	go func() {
		s.logger.Info("server starting",
			slog.Int("port", s.config.Port),
			slog.String("url", fmt.Sprintf("http://localhost:%d", s.config.Port)),
		)
		serverErrors <- srv.ListenAndServe()
	}()

	// Block until we receive a signal or server error
	select {
	case err := <-serverErrors:
		// Server failed to start
		if err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}

	case sig := <-quit:
		// Received shutdown signal
		s.logger.Info("shutdown signal received", slog.String("signal", sig.String()))

		// Give in-flight requests 30 seconds to complete
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.logger.Info("server stopped gracefully")
	}

	return nil
}
