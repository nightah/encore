package fakekodi

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
)

// NewNetwork returns an empty in-memory network; its fakes stop when the test ends.
func NewNetwork(t testing.TB) *Network {
	t.Helper()

	n := &Network{t: t, servers: map[string]*Server{}}
	n.client = &http.Client{Transport: n}

	return n
}

// New starts a fake on the network. Its URLs name a host of its own: kodi1.test for the first fake, kodi2.test for the next, and so on.
func (n *Network) New(opts Options) *Server {
	n.t.Helper()

	s := NewServer(opts)
	s.network, s.httpServer = n, httptest.NewTestServer(n.t, s)
	s.httpServer.Client() // starts the server

	n.mu.Lock()
	s.host = fmt.Sprintf("kodi%d.test", len(n.servers)+1)
	n.servers[s.host] = s
	n.mu.Unlock()

	return s
}

// Client returns an HTTP client that reaches every fake on the network, over HTTP and WebSocket alike.
func (n *Network) Client() *http.Client { return n.client }

// RoundTrip implements http.RoundTripper. A host with no fake, or one that is down, refuses the connection.
func (n *Network) RoundTrip(r *http.Request) (*http.Response, error) {
	n.mu.Lock()
	s := n.servers[r.URL.Hostname()]
	n.mu.Unlock()

	if s == nil || s.isDown() {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	}

	return s.httpServer.Client().Transport.RoundTrip(r)
}

func (s *Server) isDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.down
}

// setDown takes the fake off the network, closing every connection to it, or puts it back.
func (s *Server) setDown(down bool) {
	s.mu.Lock()
	s.down = down
	s.mu.Unlock()

	if down {
		s.DropConnections()
		s.httpServer.CloseClientConnections()
	}
}
