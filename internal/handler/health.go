package handler

import "net/http"

// HandleHealth reports that the server is up. Docker Compose polls it to know
// when the app is ready.
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
