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

// WSEvent is the shape pushed to all connected clients.
type WSEvent struct {
	Type      string `json:"type"`       // "reindex_started" | "reindex_completed" | "watcher_event" | "daemon_log"
	Group     string `json:"group"`
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
type wsClient struct {
	conn net.Conn
	send chan []byte
	done chan struct{}
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

// Broadcast sends an event to all connected clients after a 2-second debounce
// per group.  Repeated calls for the same group reset the timer.
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

	// Read pump: drain incoming frames (pings / close frames) so the OS
	// buffer doesn't fill up.  We don't need to act on them for a push-only
	// server, but we must read to detect disconnects.
	br := bufio.NewReader(conn)
	for {
		select {
		case <-client.done:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, err := readWSFrame(br)
		if err != nil {
			return
		}
	}
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
