// Package transport provides the net/http implementation of the protocol
// Transport interface — the single I/O point of the client. It is the only
// package allowed to open sockets.
package transport

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

const defaultTimeout = 120 * time.Second

// HTTPTransport speaks to the pipeline server over 127.0.0.1 with keep-alive
// connection reuse. Requests to localhost never go through a proxy.
type HTTPTransport struct {
	client *http.Client
}

// New builds a transport with a keep-alive connection pool bound to loopback.
func New() *HTTPTransport {
	return &HTTPTransport{
		client: &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				Proxy: nil, // never proxy loopback traffic
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:        4,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Do executes one request. It implements protocol.Transport.
func (t *HTTPTransport) Do(req protocol.Request) (protocol.Response, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	url := "http://" + req.HostPort + req.Path
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequest(req.Method, url, body)
	if err != nil {
		return protocol.Response{}, fmt.Errorf("build request: %w", err)
	}
	if req.AuthToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.AuthToken)
	}
	if len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		// Surface a compact connectivity error; the retry ladder in the
		// protocol layer decides what to do with it.
		return protocol.Response{}, compactHTTPError(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return protocol.Response{}, fmt.Errorf("read response: %w", err)
	}
	return protocol.Response{StatusCode: resp.StatusCode, Body: data}, nil
}

// Close releases the idle connection pool.
func (t *HTTPTransport) Close() error {
	t.client.CloseIdleConnections()
	return nil
}

// compactHTTPError strips the redundant net/http URL prefix so stderr stays
// readable: "connect 127.0.0.1:7802: connection refused".
func compactHTTPError(err error) error {
	msg := err.Error()
	for _, prefix := range []string{
		`Get "http://`, `Post "http://`,
	} {
		if strings.HasPrefix(msg, prefix) {
			if idx := strings.Index(msg, `": `); idx >= 0 {
				msg = msg[idx+3:]
			}
			break
		}
	}
	return fmt.Errorf("%s", msg)
}
