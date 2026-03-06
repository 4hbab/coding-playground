// Package server sets up the HTTP server, router, and all route definitions.
//
// SERVER ARCHITECTURE:
// This package is the "wiring" layer — it connects handlers, middleware, and routes.
// Think of it as the control centre that decides:
// - Which URL patterns map to which handler functions
// - What middleware runs on which routes
// - How the server starts and stops gracefully
//
// DEPENDENCY INJECTION FLOW (WITH AUTH):
// main.go creates:
//
//	DB path + auth config (env vars) → passed to Server
//	Server.New() creates:
//	  sqlite.DB → SnippetService → SnippetHandler
//	  TokenService + GitHubProvider → AuthService → AuthHandler
//	  OptionalAuth middleware → applied to mutating snippet routes
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
type Config struct {
	Port        int
	TemplateDir string
	StaticDir   string
	DBPath      string

	// Auth configuration (all optional — auth is disabled if JWTSecret is empty)
	JWTSecret          string
	GitHubClientID     string
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
// ROUTE STRUCTURE:
// GET    /                             → Playground page (HTML)
// GET    /static/*                     → Static files (CSS, JS, images)
//
// AUTH ROUTES (only if JWTSecret is set):
// GET    /auth/github/login            → Redirect to GitHub OAuth
// GET    /auth/github/callback         → Handle OAuth callback
// POST   /auth/logout                  → Clear JWT cookie
// GET    /api/me                       → Current user profile (RequireAuth)
//
// API ROUTES:
// GET    /api/snippets                 → List snippets
// GET    /api/snippets/{id}            → Get snippet
// POST   /api/snippets                 → Create snippet (OptionalAuth)
// PUT    /api/snippets/{id}            → Update snippet (OptionalAuth)
// DELETE /api/snippets/{id}            → Delete snippet (OptionalAuth)
// POST   /api/execute                  → Execute code (if Docker available)
func (s *Server) setupRoutes() error {
	// === Global Middleware ===
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(chimiddleware.RealIP)
	s.router.Use(chimiddleware.Recoverer)
	s.router.Use(middleware.Logger(s.logger))

	// === Static Files ===
	fileServer := http.FileServer(http.Dir(s.config.StaticDir))
	s.router.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// === Page Routes ===
	playgroundHandler, err := handler.NewPlaygroundHandler(s.config.TemplateDir, s.logger)
	if err != nil {
		return fmt.Errorf("creating playground handler: %w", err)
	}
	s.router.Get("/", playgroundHandler.HandlePlayground)

	// === Auth Setup (optional — enabled when JWTSecret is configured) ===
	var tokenService *auth.TokenService
	if s.config.JWTSecret != "" {
		ts, err := auth.NewTokenService(s.config.JWTSecret)
		if err != nil {
			return fmt.Errorf("creating token service: %w", err)
		}
		tokenService = ts

		// Only wire GitHub OAuth routes if all credentials are present
		if s.config.GitHubClientID != "" && s.config.GitHubClientSecret != "" {
			callbackURL := s.config.GitHubCallbackURL
			if callbackURL == "" {
				callbackURL = fmt.Sprintf("http://localhost:%d/auth/github/callback", s.config.Port)
			}

			githubProvider := auth.NewGitHubProvider(
				s.config.GitHubClientID,
				s.config.GitHubClientSecret,
				callbackURL,
			)

			authService := service.NewAuthService(s.db, githubProvider, tokenService, s.logger)
			authHandler := handler.NewAuthHandler(authService, githubProvider, s.logger)

			// Auth routes
			s.router.Get("/auth/github/login", authHandler.HandleGitHubLogin)
			s.router.Get("/auth/github/callback", authHandler.HandleGitHubCallback)
			s.router.Post("/auth/logout", authHandler.HandleLogout)

			s.logger.Info("GitHub OAuth enabled")
		} else {
			s.logger.Warn("JWT configured but GitHub OAuth credentials missing — auth routes disabled")
		}
	} else {
		s.logger.Warn("JWT_SECRET not set — authentication disabled")
	}

	// === API Routes ===
	snippetService := service.NewSnippetService(s.db, s.logger)
	snippetHandler := handler.NewSnippetHandler(snippetService, s.logger)

	s.router.Route("/api", func(r chi.Router) {
		// /api/me requires authentication
		if tokenService != nil {
			r.With(auth.RequireAuth(tokenService)).Get("/me", func(w http.ResponseWriter, req *http.Request) {
				// We need the auth handler for HandleMe, but it might not exist if GitHub creds are missing.
				// Create a minimal handler just for /api/me.
				userID, ok := auth.UserIDFromContext(req.Context())
				if !ok {
					http.Error(w, `{"error":"not authenticated"}`, http.StatusUnauthorized)
					return
				}
				user, err := s.db.GetUserByID(req.Context(), userID)
				if err != nil || user == nil {
					http.Error(w, `{"error":"user not found"}`, http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json := fmt.Sprintf(`{"id":"%s","login":"%s","email":"%s","avatarUrl":"%s"}`,
					user.ID, user.Login, user.Email, user.AvatarURL)
				w.Write([]byte(json))
			})
		}

		// Read-only snippet routes (no auth needed)
		r.Get("/snippets", snippetHandler.HandleList)
		r.Get("/snippets/{id}", snippetHandler.HandleGetByID)

		// Mutating snippet routes — apply OptionalAuth if available
		if tokenService != nil {
			r.With(auth.OptionalAuth(tokenService)).Post("/snippets", snippetHandler.HandleCreate)
			r.With(auth.OptionalAuth(tokenService)).Put("/snippets/{id}", snippetHandler.HandleUpdate)
			r.With(auth.OptionalAuth(tokenService)).Delete("/snippets/{id}", snippetHandler.HandleDelete)
		} else {
			r.Post("/snippets", snippetHandler.HandleCreate)
			r.Put("/snippets/{id}", snippetHandler.HandleUpdate)
			r.Delete("/snippets/{id}", snippetHandler.HandleDelete)
		}

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
