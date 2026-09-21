package server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlowJSONBodiesReturn408(t *testing.T) {
	for _, prefix := range []string{`{"name":`, `{"name":"done"} `} {
		t.Run(prefix, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Name string `json:"name"`
				}
				if readJSON(w, r, &body) {
					reply(w, 200, body)
				}
			})
			srv := httptest.NewServer(withBodyDeadlines(handler, 100*time.Millisecond, time.Second))
			defer srv.Close()
			conn, err := net.Dial("tcp", srv.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			fmt.Fprintf(conn, "POST /api/folders HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\nContent-Type: application/json\r\n\r\n%s", prefix)
			res, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != 408 || !strings.Contains(string(body), "超时") {
				t.Fatalf("got %d %s", res.StatusCode, body)
			}
		})
	}
}
func TestSlowMultipartUsesUploadDeadline(t *testing.T) {
	a := testApp(t, "")
	srv := httptest.NewServer(withBodyDeadlines(http.HandlerFunc(a.upload), 5*time.Millisecond, 150*time.Millisecond))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprint(conn, "POST /api/images HTTP/1.1\r\nHost: localhost\r\nContent-Length: 1000\r\nContent-Type: multipart/form-data; boundary=test\r\n\r\n--test\r\n")
	start := time.Now()
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 408 {
		t.Fatalf("got %d", res.StatusCode)
	}
	if time.Since(start) < 75*time.Millisecond {
		t.Fatal("upload incorrectly used the shorter JSON deadline")
	}
	var count int
	if err = a.db.QueryRow("SELECT COUNT(*) FROM images").Scan(&count); err != nil || count != 0 {
		t.Fatal("timed-out upload persisted")
	}
}
func TestBodyDeadlineDoesNotBreakNextKeepAliveRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if readJSON(w, r, &body) {
			reply(w, 200, body)
		}
	})
	srv := httptest.NewServer(withBodyDeadlines(handler, 100*time.Millisecond, time.Second))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(conn)
	for i := 0; i < 2; i++ {
		body := `{"name":"正常请求"}`
		fmt.Fprintf(conn, "POST /api/folders HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\nContent-Type: application/json\r\n\r\n%s", len(body), body)
		res, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
		if i == 0 {
			time.Sleep(180 * time.Millisecond)
		}
	}
}
func TestHTTPServerHasReadBackstop(t *testing.T) {
	s := NewHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if s.ReadTimeout <= uploadBodyTimeout || s.ReadHeaderTimeout <= 0 {
		t.Fatal("server read deadlines missing")
	}
}
