package handler

import "net/http"

// ConfigResponse is the runtime configuration the frontend needs.
//
// WHY AN ENDPOINT?
// The frontend reads its configuration from the server instead of having it
// baked into the JS files, so the same files work in every environment.
type ConfigResponse struct {
	// ServerExecution is true when POST /api/execute is available (Docker is running).
	ServerExecution bool `json:"serverExecution"`
}

// ConfigHandler serves GET /api/config.
type ConfigHandler struct {
	config ConfigResponse
}

// NewConfigHandler creates a ConfigHandler.
func NewConfigHandler(serverExecution bool) *ConfigHandler {
	return &ConfigHandler{config: ConfigResponse{ServerExecution: serverExecution}}
}

// HandleConfig returns the frontend configuration.
func (h *ConfigHandler) HandleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.config)
}
