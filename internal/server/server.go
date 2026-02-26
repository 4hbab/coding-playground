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
//
// DEPENDENCY INJECTION FLOW (UPDATED):
// main.go creates:
//   DB path (config) → passed to Server
//   Server.New() creates: sqlite.DB → SnippetService → SnippetHandler
//
// This is the "composition root" pattern — all dependencies are wired
// in one place (New/setupRoutes), rather than scattered across the codebase.
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
	sqliteRepo "github.com/sakif/coding-playground/internal/repository/sqlite"
	"github.com/sakif/coding-playground/internal/service"
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
	DBPath      string // NEW: path to the SQLite database file
}

// Server represents the HTTP server and all its dependencies.
//
// RESOURCE MANAGEMENT:
// The Server now owns a database connection (db). When the server shuts down,
// we must close this connection to flush any pending writes and release the file lock.
// This is handled in Start() during graceful shutdown.
type Server struct {
	router *chi.Mux
	config Config
	logger *slog.Logger
	db     *sqliteRepo.DB // NEW: database connection (owned by server, closed on shutdown)
}

// New creates a new Server with the given config.
//
// DEPENDENCY INJECTION & WIRING:
// This is where the entire dependency chain is assembled:
//   1. Create the database connection (sqlite.New)
//   2. Create the service layer (service.NewSnippetService) with the DB
//   3. Create the handler (handler.NewSnippetHandler) with the service
//   4. Wire handlers to routes
//
// Each layer only receives what it needs:
// - Service gets the repository interface (not the concrete sqlite.DB)
// - Handler gets the service (not the repository or DB)
//
// IMPORT ALIAS:
// We import repository/sqlite as `sqliteRepo` to avoid confusion with
// the sqlite driver package. Import aliases are common in Go when
// package names would otherwise collide or be unclear.
func New(cfg Config, logger *slog.Logger) (*Server, error) {
	// === CREATE DATABASE ===
	db, err := sqliteRepo.New(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	s := &Server{
		router: chi.NewRouter(),
		config: cfg,
		logger: logger,
		db:     db,
	}

	// Set up middleware and routes
	if err := s.setupRoutes(); err != nil {
		db.Close() // Clean up DB if route setup fails
		return nil, fmt.Errorf("setting up routes: %w", err)
	}

	return s, nil
}

// setupRoutes configures all middleware and route handlers.
//
// ROUTE STRUCTURE (UPDATED):
// GET    /                      → Playground page (HTML)
// GET    /static/*              → Static files (CSS, JS, images)
// GET    /api/snippets          → List snippets (JSON)
// GET    /api/snippets/{id}     → Get single snippet (JSON)  [NEW]
// POST   /api/snippets          → Create snippet (JSON)
// PUT    /api/snippets/{id}     → Update snippet (JSON)      [NEW]
// DELETE /api/snippets/{id}     → Delete snippet
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

	// === API Routes (UPDATED) ===
	// DEPENDENCY CHAIN:
	//   s.db (sqlite.DB) → implements repository.SnippetRepository
	//   SnippetService receives the repository interface
	//   SnippetHandler receives the service
	//
	// Notice: the handler never touches the database directly.
	// The service never touches HTTP. Clean separation!
	snippetService := service.NewSnippetService(s.db, s.logger)
	snippetHandler := handler.NewSnippetHandler(snippetService, s.logger)

	s.router.Route("/api", func(r chi.Router) {
		r.Get("/snippets", snippetHandler.HandleList)
		r.Get("/snippets/{id}", snippetHandler.HandleGetByID) // NEW
		r.Post("/snippets", snippetHandler.HandleCreate)
		r.Put("/snippets/{id}", snippetHandler.HandleUpdate) // NEW
		r.Delete("/snippets/{id}", snippetHandler.HandleDelete)
	})

	return nil
}

// Start starts the HTTP server and handles graceful shutdown.
//
// GRACEFUL SHUTDOWN (UPDATED):
// Now that we have a database connection, shutdown is more important:
// 1. Stop accepting new HTTP connections
// 2. Wait for in-flight requests to finish (30s timeout)
// 3. Close the database connection (flushes WAL, releases file lock)
//
// If we skip step 3, the database file might be left in an inconsistent state.
// The `defer s.db.Close()` ensures this happens even if something panics.
func (s *Server) Start() error {
	// Ensure the database is closed when the server stops.
	// This runs AFTER everything else in this function finishes.
	defer s.db.Close()

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
			slog.String("database", s.config.DBPath),
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
