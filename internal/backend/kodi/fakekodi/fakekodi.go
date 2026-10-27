package fakekodi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func (e *Error) Error() string { return fmt.Sprintf("kodi error %d: %s", e.Code, e.Message) }

// New starts a fake Kodi and stops it when the test ends.
func New(t testing.TB, opts Options) *Server {
	t.Helper()

	s := NewServer(opts)
	s.httpServer = httptest.NewServer(s)
	s.addr = s.httpServer.Listener.Addr().String()
	t.Cleanup(s.Close)

	return s
}

// NewServer returns an unstarted fake, for use as an http.Handler.
func NewServer(opts Options) *Server {
	if opts.Version == ([3]int{}) {
		opts.Version = [3]int{13, 5, 0}
	}

	if opts.Library == nil {
		opts.Library = DefaultLibrary()
	}

	if opts.MAC == "" {
		opts.MAC = "DC:A6:32:4B:19:0E"
	}

	s := &Server{
		opts:     opts,
		handlers: map[string]HandlerFunc{},
		clients:  map[*websocket.Conn]struct{}{},
		state:    playerState{volume: 50, playlists: map[int][]Item{}, repeat: "off", clock: "09:42:17 PM"},
		mac:      opts.MAC,
	}
	s.settled = sync.NewCond(&s.mu)
	s.registerDefaults()

	return s
}

// URL is the HTTP JSON-RPC endpoint, e.g. http://127.0.0.1:1234/jsonrpc.
func (s *Server) URL() string { return s.httpServer.URL + "/jsonrpc" }

// WebSocketURL is the WebSocket JSON-RPC endpoint.
func (s *Server) WebSocketURL() string {
	return "ws" + strings.TrimPrefix(s.httpServer.URL, "http") + "/jsonrpc"
}

// Close stops the server and disconnects WebSocket clients.
func (s *Server) Close() {
	s.DropConnections()

	if s.httpServer != nil {
		s.httpServer.Close()
	}
}

// Restart stops the server and starts it again on the same address, as when Kodi restarts. Library and player state survive.
func (s *Server) Restart(t testing.TB) {
	t.Helper()
	s.Close()

	var ln net.Listener

	// The old socket may take a moment to release the port.
	deadline := time.Now().Add(5 * time.Second)

	for {
		var err error
		if ln, err = net.Listen("tcp", s.addr); err == nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("fakekodi: relistening on %s: %v", s.addr, err)
		}

		time.Sleep(10 * time.Millisecond)
	}

	srv := httptest.NewUnstartedServer(s)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	s.httpServer = srv
}

// DropConnections disconnects every WebSocket client without stopping the server.
func (s *Server) DropConnections() {
	for _, c := range s.connected() {
		_ = c.CloseNow()
	}
}

// SetWebSocket enables or disables the WebSocket endpoint. Disabling drops connected clients and refuses new ones, leaving HTTP working.
func (s *Server) SetWebSocket(enabled bool) {
	s.mu.Lock()
	s.wsOff = !enabled
	s.mu.Unlock()

	if !enabled {
		s.DropConnections()
	}
}

// WebSocketClients reports how many WebSocket clients are connected.
func (s *Server) WebSocketClients() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.clients)
}

// Update changes the library under the server's lock and the library's own, which servers sharing the library also take.
func (s *Server) Update(fn func(*Library)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lib, unlock := s.lockLibrary()
	defer unlock()

	fn(lib)
}

// Other servers may share the library. Callers may hold s.mu; the library lock is always taken after it.
func (s *Server) lockLibrary() (*Library, func()) {
	lib := s.opts.Library
	lib.mu.Lock()

	return lib, lib.mu.Unlock
}

// SetMAC changes the reported Network.MacAddress, e.g. to "Busy" as Kodi says while it looks the address up.
func (s *Server) SetMAC(mac string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.mac = mac
}

// SetPosition moves the playhead of the active player.
func (s *Server) SetPosition(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.position = d.Seconds()
}

// SetClock sets what System.Time reports, e.g. "07:05:00 AM".
func (s *Server) SetClock(clock string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.clock = clock
}

// Handle overrides or adds the handler for method.
func (s *Server) Handle(method string, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.handlers[method] = h
}

// Calls returns every call received so far, in order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Call(nil), s.calls...)
}

// CallsTo returns the recorded calls to one method.
func (s *Server) CallsTo(method string) []Call {
	var out []Call

	for _, c := range s.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}

	return out
}

// Notify sends a notification to every connected WebSocket client.
func (s *Server) Notify(method string, data any) {
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  map[string]any{"sender": "xbmc", "data": data},
	})
	if err != nil {
		panic(err) // a test passed something unmarshalable
	}

	for _, c := range s.connected() {
		_ = c.Write(context.Background(), websocket.MessageText, msg)
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/image/") {
		s.serveImage(w, r)

		return
	}

	if r.URL.Path != "/jsonrpc" {
		http.NotFound(w, r)

		return
	}

	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.mu.Lock()
		off := s.wsOff
		s.mu.Unlock()

		if off {
			http.Error(w, "websocket disabled", http.StatusServiceUnavailable)

			return
		}

		s.serveWebSocket(w, r)

		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if s.opts.Username != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.opts.Username || pass != s.opts.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="XBMC"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeResponse(w, response{JSONRPC: "2.0", Error: &Error{Code: -32700, Message: "Parse error."}})

		return
	}

	writeResponse(w, s.dispatch(req, false))
}

// connected returns the WebSocket clients, once every handshake under way has finished. A client counts itself connected as soon as it reads the handshake response, which Accept sends before it returns, so without the wait a notification sent straight after could miss it.
func (s *Server) connected() []*websocket.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()

	for s.accepting > 0 {
		s.settled.Wait()
	}

	clients := make([]*websocket.Conn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}

	return clients
}

func (s *Server) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.accepting++
	s.mu.Unlock()

	conn, err := websocket.Accept(w, r, nil)
	if s.afterAccept != nil {
		s.afterAccept()
	}

	s.mu.Lock()
	s.accepting--
	if err == nil {
		s.clients[conn] = struct{}{}
	}
	s.settled.Broadcast()
	s.mu.Unlock()

	if err != nil {
		return
	}

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
	}()

	ctx := r.Context()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var req request
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}

		// Answer concurrently so responses can overtake each other, which clients must cope with.
		go func() {
			resp, err := json.Marshal(s.dispatch(req, true))
			if err == nil {
				_ = conn.Write(ctx, websocket.MessageText, resp)
			}
		}()
	}
}

func (s *Server) dispatch(req request, ws bool) response {
	s.mu.Lock()
	s.calls = append(s.calls, Call{Method: req.Method, Params: req.Params, WebSocket: ws})
	h, ok := s.handlers[req.Method]
	s.mu.Unlock()

	resp := response{JSONRPC: "2.0", ID: req.ID}

	if !ok {
		resp.Error = &Error{Code: CodeMethodNotFound, Message: "Method not found."}

		return resp
	}

	result, rpcErr := h(req.Params)
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}

	return resp
}

func writeResponse(w http.ResponseWriter, resp response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func decode(params json.RawMessage, v any) *Error {
	if len(params) == 0 {
		return nil
	}

	if err := json.Unmarshal(params, v); err != nil {
		return &Error{Code: CodeInvalidParams, Message: "Invalid params: " + err.Error()}
	}

	return nil
}

// A 1x1 transparent PNG, served for every image:// reference.
var fakePNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

// Kodi serves /image/<url-encoded image:// reference> behind the same credentials as JSON-RPC over HTTP.
func (s *Server) serveImage(w http.ResponseWriter, r *http.Request) {
	if s.opts.Username != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.opts.Username || pass != s.opts.Password {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}
	}

	ref, err := url.QueryUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/image/"))
	if err != nil || !strings.HasPrefix(ref, "image://fake/") {
		http.NotFound(w, r)

		return
	}

	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(fakePNG)
}
