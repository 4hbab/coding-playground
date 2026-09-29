package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		header     string // configured trusted header ("" = none)
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{name: "no trusted header uses the connection address", remoteAddr: "203.0.113.7:51234", want: "203.0.113.7"},
		{name: "IPv6 connection address", remoteAddr: "[2001:db8::1]:443", want: "2001:db8::1"},
		{
			name: "client-sent forwarding headers are ignored when nothing is trusted", remoteAddr: "203.0.113.7:1",
			headers: map[string]string{"X-Forwarded-For": "1.1.1.1", "X-Real-IP": "1.1.1.1", "True-Client-IP": "1.1.1.1"},
			want:    "203.0.113.7",
		},
		{
			name: "trusted header set by the proxy is used", header: "X-Client-IP", remoteAddr: "172.18.0.3:40000",
			headers: map[string]string{"X-Client-IP": "198.51.100.20"}, want: "198.51.100.20",
		},
		{
			name: "only the trusted header counts", header: "X-Client-IP", remoteAddr: "172.18.0.3:40000",
			headers: map[string]string{"X-Client-IP": "198.51.100.20", "X-Forwarded-For": "1.1.1.1"}, want: "198.51.100.20",
		},
		{
			name: "missing trusted header falls back to the connection", header: "X-Client-IP", remoteAddr: "127.0.0.1:9999",
			want: "127.0.0.1",
		},
		{
			name: "garbage in the trusted header falls back to the connection", header: "X-Client-IP", remoteAddr: "127.0.0.1:9999",
			headers: map[string]string{"X-Client-IP": "not-an-ip"}, want: "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := ClientIP(tt.header)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.RemoteAddr
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tt.want, got)
		})
	}
}
