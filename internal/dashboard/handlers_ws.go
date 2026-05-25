package dashboard

// handlers_ws.go — WebSocket push endpoint
//
//	/ws/events
//
// The server sends JSON events when a group is re-indexed or a watcher
// fires.  Events are debounced server-side with a 2-second trailing-edge
// delay per group to avoid flooding clients during fast file edits.
//
// This implementation uses stdlib net/http + manual HTTP upgrade (no
// third-party WebSocket library) to keep the dashboard binary slim.
// The upgrade speaks the WebSocket framing protocol at the minimum level
// needed for text-frame push from server to client.  Clients that need
// full duplex should use a proper WS library on the frontend side.

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WSEvent is the shape pushed to connected clients.
//
// Every event carries a (Group, Ref) tuple so the server-side filter can
// decide which per-connection subscriptions match the event. Ref is the git
// branch / tag name (e.g. "main", "feature/foo"). An empty Ref means "current
// HEAD / no-ref context" and is treated like a wildcard by the filter — it
// matches every subscription.
type WSEvent struct {
	Type      string `json:"type"` // "reindex_started" | "reindex_completed" | "watcher_event" | "daemon_log"
	Group     string `json:"group"`
	Ref       string `json:"ref,omitempty"`
	Repo      string `json:"repo,omitempty"`
	Path      string `json:"path,omitempty"`
	Timestamp string `json:"timestamp"`
}

// wsHub manages all active WebSocket connections.
type wsHub struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
	// debounce timers: group -> pending timer
	debounce map[string]*time.Timer
}

// wsClient wraps a single WebSocket connection.
//
// sub holds the optional per-connection subscription filter set by a
// "subscribe" client→server message. A nil sub means the client receives all
// events (firehose mode, backward-compatible default). A non-nil sub means
// only events matching the filter are delivered. See wsFilter in ws_filter.go
// for the matching semantics.
type wsClient struct {
	conn net.Conn
	send chan []byte
	done chan struct{}

	subMu sync.Mutex
	sub   *wsFilter // nil = firehose (default)
}

func newWSHub() *wsHub {
	return &wsHub{
		clients:  map[*wsClient]struct{}{},
		debounce: map[string]*time.Timer{},
	}
}

// run is the hub's event loop.  Call in a goroutine.
func (h *wsHub) run() {
	// The hub itself is passive — clients register/unregister themselves
	// via add/remove.  No separate channel needed for this lightweight impl.
}

func (h *wsHub) add(c *wsClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *wsHub) remove(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast sends an event to connected clients after a 2-second debounce per
// group. Repeated calls for the same group reset the timer.
//
// Filtering: each client's subscription (set via a "subscribe" message) is
// consulted before delivery. Clients that never sent a "subscribe" message
// receive all events (firehose, backward-compatible). See wsFilter.Matches for
// the exact matching semantics.
func (h *wsHub) Broadcast(evt WSEvent) {
	h.mu.Lock()
	key := evt.Group + "/" + evt.Type
	if t, ok := h.debounce[key]; ok {
		t.Stop()
	}
	h.debounce[key] = time.AfterFunc(2*time.Second, func() {
		h.mu.Lock()
		delete(h.debounce, key)
		clients := make([]*wsClient, 0, len(h.clients))
		for c := range h.clients {
			clients = append(clients, c)
		}
		h.mu.Unlock()

		evt.Timestamp = time.Now().UTC().Format(time.RFC3339)
		data, _ := json.Marshal(evt)
		frame := wsTextFrame(data)
		for _, c := range clients {
			// Apply per-connection subscription filter.
			c.subMu.Lock()
			f := c.sub
			c.subMu.Unlock()
			if f != nil && !f.Matches(evt) {
				continue // filtered out for this client
			}
			select {
			case c.send <- frame:
			default:
				// client too slow; drop
			}
		}
	})
	h.mu.Unlock()
}

// handleWSEvents upgrades the HTTP connection to a WebSocket and streams
// events to the client until it disconnects.
func (s *Server) handleWSEvents(w http.ResponseWriter, r *http.Request) {
	// Only upgrade if the client sent a valid WebSocket handshake.
	if !isWSUpgrade(r) {
		http.Error(w, "WebSocket upgrade required", http.StatusUpgradeRequired)
		return
	}
	key := r.Header.Get("Sec-Websocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}

	// Hijack the connection.
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "server does not support hijack", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return
	}

	// Send the 101 Switching Protocols response.
	accept := wsAcceptKey(key)
	resp := fmt.Sprintf(
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n\r\n",
		accept,
	)
	if _, err := io.WriteString(bufrw, resp); err != nil {
		conn.Close()
		return
	}
	if err := bufrw.Flush(); err != nil {
		conn.Close()
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 32),
		done: make(chan struct{}),
	}
	s.hub.add(client)
	defer func() {
		s.hub.remove(client)
		conn.Close()
	}()

	// Write pump: drain client.send and write frames.
	go func() {
		for {
			select {
			case frame, ok := <-client.send:
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, err := conn.Write(frame); err != nil {
					close(client.done)
					return
				}
			case <-client.done:
				return
			}
		}
	}()

	// Read pump: read incoming frames from the client.
	//
	// We handle two client→server message types (see wsClientMsg):
	//   - {"type":"subscribe","groups":[...],"refs":[...]} — install a filter.
	//   - {"type":"unsubscribe"}                           — remove the filter (firehose).
	//
	// All other frames (pings, browser keep-alives) are drained and discarded so
	// the OS read buffer never fills and disconnects are detected promptly.
	br := bufio.NewReader(conn)
	for {
		select {
		case <-client.done:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		payload, err := readWSFrame(br)
		if err != nil {
			return
		}
		if len(payload) == 0 {
			continue
		}
		var msg wsClientMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue // not JSON or wrong shape — ignore
		}
		switch msg.Type {
		case "subscribe":
			f := newWSFilter(msg.Groups, msg.Refs)
			client.subMu.Lock()
			client.sub = f
			client.subMu.Unlock()
		case "unsubscribe":
			client.subMu.Lock()
			client.sub = nil
			client.subMu.Unlock()
		}
	}
}

// wsClientMsg is a client→server WebSocket control message.
//
// Schema:
//
//	Subscribe (install or replace filter):
//	  {"type":"subscribe","groups":["g1","g2"],"refs":["main","feat/x"]}
//
//	  Semantics: the connection will only receive events where
//	    (event.Group ∈ groups OR groups is empty)
//	    AND
//	    (event.Ref ∈ refs OR refs is empty OR event.Ref == "")
//	  Calling subscribe again REPLACES the prior filter (not appends).
//
//	Unsubscribe (clear filter → firehose):
//	  {"type":"unsubscribe"}
type wsClientMsg struct {
	Type   string   `json:"type"`             // "subscribe" | "unsubscribe"
	Groups []string `json:"groups,omitempty"` // group names to subscribe to
	Refs   []string `json:"refs,omitempty"`   // ref names to subscribe to
}

// isWSUpgrade returns true if the request is a WebSocket upgrade.
func isWSUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// wsAcceptKey computes the Sec-WebSocket-Accept response header value.
func wsAcceptKey(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// wsTextFrame encodes data as a WebSocket text frame (final, no mask).
func wsTextFrame(data []byte) []byte {
	var buf bytes.Buffer
	// FIN=1, opcode=1 (text)
	buf.WriteByte(0x81)
	l := len(data)
	switch {
	case l <= 125:
		buf.WriteByte(byte(l))
	case l <= 0xFFFF:
		buf.WriteByte(126)
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(l))
		buf.Write(b)
	default:
		buf.WriteByte(127)
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(l))
		buf.Write(b)
	}
	buf.Write(data)
	return buf.Bytes()
}

// readWSFrame reads one WebSocket frame from r (minimal; ignores payload).
func readWSFrame(r *bufio.Reader) ([]byte, error) {
	// Read the first two bytes (FIN/opcode + mask/payload-len).
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	masked := header[1]&0x80 != 0
	payLen := int(header[1] & 0x7F)
	switch payLen {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		payLen = int(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		payLen = int(binary.BigEndian.Uint64(ext))
	}
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, payLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	// Opcode 8 = close frame.
	if header[0]&0x0F == 8 {
		return nil, io.EOF
	}
	return payload, nil
}
