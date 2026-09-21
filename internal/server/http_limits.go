package server

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

const jsonBodyTimeout = 15 * time.Second
const uploadBodyTimeout = 4 * time.Minute

// The connection-level timeout is a backstop. Route-specific deadlines start
// after headers and leave time within the browser's five-minute upload timeout.
func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
}
func withBodyDeadlines(next http.Handler, jsonLimit, uploadLimit time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != "GET" && r.Method != "HEAD" {
			limit := jsonLimit
			if r.Method == "POST" && r.URL.Path == "/api/images" {
				limit = uploadLimit
			}
			err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(limit))
			if err != nil && !errors.Is(err, http.ErrNotSupported) {
				internal(w, err)
				return
			}
			// Do not clear an expired deadline before net/http closes/drains the body.
			// net/http sets the next request's deadline when reusing the connection.
		}
		next.ServeHTTP(w, r)
	})
}
func bodyReadError(w http.ResponseWriter, err error, fallback string) {
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		w.Header().Set("Connection", "close")
		fail(w, http.StatusRequestTimeout, "请求体读取超时，请检查网络后重试")
		return
	}
	fail(w, http.StatusBadRequest, fallback)
}
