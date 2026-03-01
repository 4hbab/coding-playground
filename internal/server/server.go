// Package server sets up the HTTP server, router, and all route definitions.
//
// SERVER ARCHITECTURE:
// This package is the "wiring" layer — it connects handlers, middleware, and routes.
// Think of it as the control centre that decides:
// - Which URL patterns map to which handler functions
// - What middleware runs on which routes
// - How the server starts and stops gracefully
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

	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/executor"
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
	DBPath      string

	// Auth configuration (Phase 3)
	JWTSecret          string // required — signs JWT access tokens
	GitHubClientID     string // required for OAuth (empty = OAuth disabled)
	GitHubClientSecret string
	GitHubCallbackURL  string
}

// Server represents the HTTP server and all its dependencies.
type Server struct {
	router *chi.Mux
	config Config
	logger *slog.Logger
	db     *sqliteRepo.DB
	exec   executor.Executor
}

// New creates a new Server with the given config.
//
// DEPENDENCY INJECTION & WIRING:
// This is where the entire dependency chain is assembled:
//  1. Create the DB connection (sqlite.New)
//  2. Create the service layer with the DB
//  3. Create handlers with the services
//  4. Wire handlers to routes
func New(cfg Config, logger *slog.Logger, exec executor.Executor) (*Server, error) {
	db, err := sqliteRepo.New(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	s := &Server{
		router: chi.NewRouter(),
		config: cfg,
		logger: logger,
		db:     db,
		exec:   exec,
	}

	if err := s.setupRoutes(); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting up routes: %w", err)
	}

	return s, nil
}

// setupRoutes configures all middleware and route handlers.
//
// ROUTE STRUCTURE (Phase 3):
//
//	GET    /                          → Playground (HTML)
//	GET    /static/*                  → Static files
//	GET    /auth/github/login         → Redirect to GitHub OAuth
//	GET    /auth/github/callback      → OAuth callback, sets JWT cookie
//	POST   /auth/logout               → Clears JWT cookie
//	GET    /api/me                    → Current user profile (requires auth)
//	GET    /api/snippets              → List snippets (public)
//	GET    /api/snippets/{id}         → Get snippet (public)
//	POST   /api/snippets              → Create snippet (optional auth — sets owner)
//	PUT    /api/snippets/{id}         → Update snippet (optional auth — ownership check)
//	DELETE /api/snippets/{id}         → Delete snippet (optional auth — ownership check)
//	POST   /api/execute               → Execute code
func (s *Server) setupRoutes() error {
	// === Global Middleware ===
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(chimiddleware.RealIP)
	s.router.Use(chimiddleware.Recoverer)
	s.router.Use(middleware.Logger(s.logger))

	// === Static Files ===
	fileServer := http.FileServer(http.Dir(s.config.StaticDir))
	s.router.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// === Playground Page ===
	playgroundHandler, err := handler.NewPlaygroundHandler(s.config.TemplateDir, s.logger)
	if err != nil {
		return fmt.Errorf("creating playground handler: %w", err)
	}
	s.router.Get("/", playgroundHandler.HandlePlayground)

	// === Auth Setup ===
	// Build TokenService — required for both issuing and validating JWTs.
	// If no JWT secret is configured, skip OAuth wiring but still allow the
	// server to start (tokens just won't be issuable, and optional auth is a no-op).
	var tokenService *auth.TokenService
	if s.config.JWTSecret != "" {
		tokenService, err = auth.NewTokenService(s.config.JWTSecret)
		if err != nil {
			return fmt.Errorf("creating token service: %w", err)
		}
	}

	// === Auth Routes (GitHub OAuth) ===
	// Only register if GitHub credentials are provided.
	if s.config.GitHubClientID != "" && tokenService != nil {
		githubProvider := auth.NewGitHubProvider(
			s.config.GitHubClientID,
			s.config.GitHubClientSecret,
			s.config.GitHubCallbackURL,
		)

		authHandler := handler.NewAuthHandler(
			githubProvider,
			tokenService,
			s.db, // *sqliteRepo.DB implements repository.UserRepository
			s.logger,
		)

		s.router.Get("/auth/github/login", authHandler.HandleGitHubLogin)
		s.router.Get("/auth/github/callback", authHandler.HandleGitHubCallback)
		s.router.Post("/auth/logout", authHandler.HandleLogout)

		// GET /api/me — only for authenticated users
		s.router.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth(tokenService))
			r.Get("/api/me", authHandler.HandleMe)
		})

		s.logger.Info("GitHub OAuth enabled",
			slog.String("callbackURL", s.config.GitHubCallbackURL),
		)
	} else {
		s.logger.Warn("GitHub OAuth disabled — set GITHUB_CLIENT_ID and JWT_SECRET to enable")
	}

	// === Snippet API ===
	snippetService := service.NewSnippetService(s.db, s.logger)
	snippetHandler := handler.NewSnippetHandler(snippetService, s.logger)

	s.router.Route("/api", func(r chi.Router) {
		// Public read routes — no auth required
		r.Get("/snippets", snippetHandler.HandleList)
		r.Get("/snippets/{id}", snippetHandler.HandleGetByID)

		// Mutating routes — OptionalAuth so userID is injected when present.
		// The service layer enforces ownership rules using that userID.
		r.Group(func(r chi.Router) {
			if tokenService != nil {
				r.Use(auth.OptionalAuth(tokenService))
			}
			r.Post("/snippets", snippetHandler.HandleCreate)
			r.Put("/snippets/{id}", snippetHandler.HandleUpdate)
			r.Delete("/snippets/{id}", snippetHandler.HandleDelete)
		})

		// /api/execute only available when Docker executor is running
		if s.exec != nil {
			executeHandler := handler.NewExecuteHandler(s.exec, s.logger)
			r.Post("/execute", executeHandler.HandleExecute)
		}
	})

	return nil
}

// Start starts the HTTP server and handles graceful shutdown.
func (s *Server) Start() error {
	defer s.db.Close()

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	serverErrors := make(chan error, 1)

	go func() {
		s.logger.Info("server starting",
			slog.Int("port", s.config.Port),
			slog.String("url", fmt.Sprintf("http://localhost:%d", s.config.Port)),
			slog.String("database", s.config.DBPath),
		)
		serverErrors <- srv.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}

	case sig := <-quit:
		s.logger.Info("shutdown signal received", slog.String("signal", sig.String()))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.logger.Info("server stopped gracefully")
	}

	return nil
}
