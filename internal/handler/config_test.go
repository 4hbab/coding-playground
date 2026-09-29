package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sakif/coding-playground/internal/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigHandler(t *testing.T) {
	for _, serverExecution := range []bool{true, false} {
		h := handler.NewConfigHandler(serverExecution)

		rr := httptest.NewRecorder()
		h.HandleConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))

		require.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		// Only the documented fields are exposed.
		assert.Equal(t, map[string]any{"serverExecution": serverExecution}, body)
	}
}
