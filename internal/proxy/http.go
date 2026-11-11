package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (l *listener) serveHTTP(c *bufConn, cfg *Config) {
	var tr *http.Transport
	defer func() {
		if tr != nil {
			tr.CloseIdleConnections()
		}
	}()
	for {
		_ = c.SetReadDeadline(time.Now().Add(120 * time.Second))
		req, err := http.ReadRequest(c.r)
		if err != nil {
			return
		}
		cfg = l.cfg.Load() // pick up config changes between keep-alive requests
		if cfg.authRequired() {
			u, p, ok := proxyBasicAuth(req)
			if !ok || !cfg.checkAuth(u, p) {
				writeSimple(c, http.StatusProxyAuthRequired, "Proxy-Authenticate: Basic realm=\"proxy\"\r\n", "proxy authentication required")
				_, _ = io.Copy(io.Discard, req.Body)
				if req.Close {
					return
				}
				continue
			}
		}
		if req.Method == http.MethodConnect {
			host, port, err := splitHostPort(req.Host, 443)
			if err != nil {
				writeSimple(c, http.StatusBadRequest, "", "bad CONNECT target")
				return
			}
			target, err := l.dial(context.Background(), cfg, host, port)
			if err != nil {
				writeSimple(c, httpErrStatus(err), "", err.Error())
				return
			}
			_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
			l.pipe(c, target)
			return
		}
		// plain HTTP forwarding (absolute-form URL)
		if req.URL == nil || req.URL.Host == "" || req.URL.Scheme != "http" {
			writeSimple(c, http.StatusBadRequest, "", "this is a proxy; use absolute http:// URLs or CONNECT")
			return
		}
		if tr == nil {
			tr = &http.Transport{
				DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
					h, p, err := splitHostPort(addr, 80)
					if err != nil {
						return nil, err
					}
					cc, err := l.dial(ctx, l.cfg.Load(), h, p)
					if err != nil {
						return nil, err
					}
					return &countConn{Conn: cc, l: l}, nil
				},
				Proxy:                 nil,
				DisableCompression:    true,
				ResponseHeaderTimeout: 60 * time.Second,
				IdleConnTimeout:       60 * time.Second,
			}
		}
		removeHopHeaders(req.Header)
		req.RequestURI = ""
		resp, err := tr.RoundTrip(req)
		if err != nil {
			writeSimple(c, httpErrStatus(err), "", err.Error())
			return
		}
		removeHopHeaders(resp.Header)
		_ = c.SetWriteDeadline(time.Now().Add(10 * time.Minute))
		werr := resp.Write(c)
		resp.Body.Close()
		if werr != nil || req.Close || resp.Close {
			return
		}
	}
}

// countConn counts bytes for plain HTTP forwarding (CONNECT uses pipe()).
type countConn struct {
	net.Conn
	l *listener
}

func (c *countConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.l.counters.Down.Add(int64(n))
	return n, err
}

func (c *countConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.l.counters.Up.Add(int64(n))
	return n, err
}

func proxyBasicAuth(r *http.Request) (string, string, bool) {
	h := r.Header.Get("Proxy-Authorization")
	const prefix = "Basic "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", "", false
	}
	b, err := base64.StdEncoding.DecodeString(h[len(prefix):])
	if err != nil {
		return "", "", false
	}
	u, p, ok := strings.Cut(string(b), ":")
	return u, p, ok
}

var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func removeHopHeaders(h http.Header) {
	for _, f := range h.Values("Connection") {
		for _, k := range strings.Split(f, ",") {
			if k = strings.TrimSpace(k); k != "" {
				h.Del(k)
			}
		}
	}
	for _, k := range hopHeaders {
		h.Del(k)
	}
}

func splitHostPort(hp string, def int) (string, int, error) {
	host, ps, err := net.SplitHostPort(hp)
	if err != nil {
		// no port
		return strings.Trim(hp, "[]"), def, nil
	}
	p, err := strconv.Atoi(ps)
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, errors.New("bad port")
	}
	return host, p, nil
}

func httpErrStatus(err error) int {
	if errors.Is(err, ErrBlockedByRule) {
		return http.StatusForbidden
	}
	if errors.Is(err, ErrNoIPv6) {
		return http.StatusBadGateway
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func writeSimple(w io.Writer, code int, extraHeaders, body string) {
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "HTTP/1.1 %d %s\r\n%sContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\n\r\n%s",
		code, http.StatusText(code), extraHeaders, len(body), body)
	_ = bw.Flush()
}
