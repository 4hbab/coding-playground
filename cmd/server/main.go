// Package main is the entry point for the coding playground server.
//
// MAIN PACKAGE IN GO:
// Every Go program starts execution in the main() function of the "main" package.
// The main package should be kept minimal — its job is to:
// 1. Read configuration (from env vars, flags, or config files)
// 2. Create dependencies (logger, database connections, etc.)
// 3. Start the application
//
// All actual logic lives in imported packages (internal/server, internal/handler, etc.).
// This separation makes the app testable and its components reusable.
//
// WHY cmd/server/?
// The cmd/ directory is a Go convention for executable entry points.
// A project might have multiple executables (e.g., cmd/server, cmd/migrate, cmd/cli).
// Each gets its own directory with its own main.go.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sakif/coding-playground/internal/executor/docker"
	"github.com/sakif/coding-playground/internal/server"
)

func main() {
	// === 1. SET UP LOGGING ===
	// slog.New creates a structured logger. slog.NewTextHandler outputs human-readable logs.
	// os.Stdout means logs go to the terminal. slog.LevelDebug enables all log levels.
	//
	// Log levels (from least to most severe): Debug → Info → Warn → Error
	// In production, you'd use LevelInfo or LevelWarn to reduce noise.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// === 2. READ CONFIGURATION ===
	// We read the port from the PORT environment variable, defaulting to 8080.
	// os.Getenv returns "" if the variable isn't set, so we check and provide a default.
	//
	// In a larger app, you'd use a config library (like viper) or a config struct
	// loaded from a YAML/TOML file. For learning, env vars are simple and standard.
	port := 8080
	if portStr := os.Getenv("PORT"); portStr != "" {
		var err error
		port, err = strconv.Atoi(portStr) // Atoi = ASCII to Integer
		if err != nil {
			logger.Error("invalid PORT value", slog.String("value", portStr))
			os.Exit(1)
		}
	}

	// === 3. RESOLVE FILE PATHS ===
	// We need to find the template and static file directories relative to
	// where the binary is run from. filepath.Abs converts a relative path to absolute.
	//
	// The "web" directory is at the project root, so we go up from cmd/server/.
	// However, when running with `go run`, the working directory is usually the project root,
	// so "web/templates" and "web/static" work directly.
	templateDir, _ := filepath.Abs("web/templates")
	staticDir, _ := filepath.Abs("web/static")

	// === 4. DATABASE PATH ===
	// Default to "data/playground.db" in the project root.
	// The "data" directory will be created automatically by os.MkdirAll if it doesn't exist.
	//
	// DB_PATH env var allows overriding for production deployments.
	// Example: DB_PATH=/var/lib/playground/prod.db
	dbPath := "data/playground.db"
	if envDB := os.Getenv("DB_PATH"); envDB != "" {
		dbPath = envDB
	}

	// Ensure the data directory exists.
	// os.MkdirAll creates all parent directories if needed (like `mkdir -p`).
	// 0755 = owner can read/write/execute, others can read/execute.
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		logger.Error("failed to create database directory",
			slog.String("dir", dbDir),
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	// === 5. INITIALIZE EXECUTOR ===
	// Docker executor is optional — the server starts without it but /api/execute will be unavailable.
	exec, err := docker.New(docker.DefaultConfig(), logger)
	if err != nil {
		logger.Warn("Docker executor unavailable — /api/execute will return errors",
			slog.String("error", err.Error()),
		)
		exec = nil
	} else {
		defer exec.Close()
	}

	// === 6. AUTH CONFIGURATION ===
	// JWT_SECRET must be a long random string. Use:
	//   JWT_SECRET=$(openssl rand -hex 32)
	// If unset, auth is disabled (server still starts, OAuth routes not registered).
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		logger.Warn("JWT_SECRET not set — authentication is disabled")
	}

	githubClientID := os.Getenv("GITHUB_CLIENT_ID")
	githubClientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	githubCallbackURL := os.Getenv("GITHUB_CALLBACK_URL")
	if githubCallbackURL == "" {
		githubCallbackURL = fmt.Sprintf("http://localhost:%d/auth/github/callback", port)
	}

	// === 7. CREATE AND START THE SERVER ===
	cfg := server.Config{
		Port:               port,
		TemplateDir:        templateDir,
		StaticDir:          staticDir,
		DBPath:             dbPath,
		JWTSecret:          jwtSecret,
		GitHubClientID:     githubClientID,
		GitHubClientSecret: githubClientSecret,
		GitHubCallbackURL:  githubCallbackURL,
	}

	srv, err := server.New(cfg, logger, exec)
	if err != nil {
		logger.Error("failed to create server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Start() blocks until the server is shut down (via Ctrl+C or SIGTERM)
	if err := srv.Start(); err != nil {
		logger.Error("server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
