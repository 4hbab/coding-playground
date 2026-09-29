package middleware

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP replaces r.RemoteAddr with the client's IP address (without a port),
// so rate limiting and logging see the real visitor.
//
// WHY NOT JUST TRUST X-Forwarded-For?
// Anyone can send X-Forwarded-For, X-Real-IP or True-Client-IP, so trusting them
// blindly lets a client pick its own IP and dodge rate limits. We only trust ONE
// header, and only when it's configured: in production the Cloudflare Worker in
// front of the app sets it (overwriting whatever the visitor sent), and the app is
// reachable only through that Worker. With no header configured, the connection's
// own address is used.
func ClientIP(trustedHeader string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := hostOnly(r.RemoteAddr)
			if trustedHeader != "" {
				if v := strings.TrimSpace(r.Header.Get(trustedHeader)); net.ParseIP(v) != nil {
					ip = v
				}
			}
			r.RemoteAddr = ip
			next.ServeHTTP(w, r)
		})
	}
}

// hostOnly strips the port from "ip:port" (or "[ipv6]:port").
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
