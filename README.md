# 🐍 PyPlayground — Python Coding Playground

A browser-based Python coding playground built with **Go** (backend) and **Pyodide** (client-side Python WASM). Write, run, and debug Python code directly in your browser.

<!-- ## 🎬 Demo -->

<!-- ![Feature demo recording](demo.webp) -->

## 📸 Screenshots

### Dark Theme — Code Execution
![Dark theme with Python output](screenshots/dark-theme.png)

### Light Theme
![Light theme](screenshots/light-theme.png)

### Error Handling — Full Tracebacks
![Error handling with ZeroDivisionError](screenshots/error-handling.png)

### Keyboard Shortcuts
![Keyboard shortcuts modal](screenshots/shortcuts-modal.png)

## 🚀 Quick Start

```bash
# Run the development server
go run ./cmd/server/main.go

# Open in your browser
# http://localhost:8080
```

## 🏗️ Architecture

```
┌──────────────────────────────────────────────┐
│                  Browser                      │
│                                               │
│  ┌───────────┐  ┌───────────┐  ┌──────────┐ │
│  │  Monaco    │  │ Pyodide   │  │ Local    │ │
│  │  Editor    │  │ Web Worker│  │ Storage  │ │
│  │  (Code)    │  │ (Python)  │  │(Snippets)│ │
│  └─────┬─────┘  └─────┬─────┘  └────┬─────┘ │
│        │              │              │        │
│        └──────┬───────┴──────┬───────┘        │
│               │  app.js      │                │
│               │ (Controller) │                │
└───────────────┼──────────────┼────────────────┘
                │   HTTP       │
                ▼              │
┌───────────────────────────────────────────────┐
│              Go Server (Chi)                   │
│                                                │
│  cmd/server/main.go                            │
│  ├── internal/server/server.go  (Router)       │
│  ├── internal/handler/          (Handlers)     │
│  ├── internal/middleware/       (Logging)       │
│  └── web/                       (Templates+CSS)│
└────────────────────────────────────────────────┘
```

## ✨ Features

- **Python Execution** — Run Python code in your browser via Pyodide WASM
- **Monaco Editor** — VS Code-grade editor with syntax highlighting
- **Snippet Storage** — Save/load code snippets to localStorage
- **Error Display** — Python tracebacks with line numbers
- **Execution Timeout** — Prevents infinite loops from freezing the browser
- **Dark/Light Theme** — Toggle with a click
- **Keyboard Shortcuts** — Ctrl+Enter to run, Ctrl+S to save

## 📂 Project Structure

| Directory | Purpose |
|-----------|---------|
| `cmd/server/` | Application entry point |
| `internal/handler/` | HTTP request handlers |
| `internal/middleware/` | Request logging middleware |
| `internal/model/` | Data structures |
| `internal/server/` | Router and server setup |
| `web/templates/` | Go HTML templates |
| `web/static/` | CSS, JS, and assets |

## 🧠 Go Concepts Covered

- HTTP server with Chi router
- Middleware pattern (logging, recovery)
- `html/template` composition
- `encoding/json` marshalling/unmarshalling
- Goroutines and channels
- Graceful shutdown with signals
- `slog` structured logging
- Struct embedding and interfaces
- Error handling patterns

## 📝 License

MIT
