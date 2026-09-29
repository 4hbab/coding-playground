package middleware

import (
	"net/http"
	"strings"
)

// contentSecurityPolicy follows an allowlist: deny everything by default
// (default-src 'none'), then allow only what the playground actually loads.
//
//   - Monaco editor: scripts, styles and fonts from cdnjs
//   - Pyodide: scripts and runtime files from jsdelivr; 'wasm-unsafe-eval' lets the
//     browser compile WebAssembly (it does NOT allow JavaScript eval)
//   - Google Fonts, and GitHub avatars for signed-in users
//
// style-src allows 'unsafe-inline' because Monaco injects <style> tags and the
// templates use style attributes. Inline scripts stay blocked, which is what
// protects against XSS.
var contentSecurityPolicy = strings.Join([]string{
	"default-src 'none'",
	"script-src 'self' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net 'wasm-unsafe-eval'",
	"style-src 'self' 'unsafe-inline' https://cdnjs.cloudflare.com https://fonts.googleapis.com",
	"font-src https://fonts.gstatic.com https://cdnjs.cloudflare.com",
	"img-src 'self' data: https://avatars.githubusercontent.com",
	"connect-src 'self' https://cdn.jsdelivr.net",
	"worker-src 'self' blob:", // Monaco starts its language worker from a blob: URL
	"base-uri 'none'",
	"form-action 'self'",
	"frame-ancestors 'none'",
}, "; ")

// SecurityHeaders sets browser security headers on every response.
// hsts should be true only when the site is served over HTTPS.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY") // older browsers; frame-ancestors covers the rest
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if hsts {
				// No includeSubDomains/preload: the site lives on a workers.dev
				// subdomain we don't own, so it can't join the preload list.
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}
