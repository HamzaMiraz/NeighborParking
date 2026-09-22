package realtime

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"neighborparking/internal/domain"

	"github.com/gorilla/websocket"
)

type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	userID      string
	communityID string
	closed      atomic.Bool
}

type publish struct {
	communityID string
	data        []byte
}
type revoke struct{ communityID, userID string }

type Hub struct {
	register   chan *Client
	unregister chan *Client
	publish    chan publish
	revoke     chan revoke
	clients    map[string]map[*Client]bool
	done       chan struct{}
	log        *slog.Logger
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{register: make(chan *Client), unregister: make(chan *Client), publish: make(chan publish, 256), revoke: make(chan revoke, 32), clients: make(map[string]map[*Client]bool), done: make(chan struct{}), log: log}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			if h.clients[c.communityID] == nil {
				h.clients[c.communityID] = make(map[*Client]bool)
			}
			h.clients[c.communityID][c] = true
		case c := <-h.unregister:
			h.remove(c)
		case p := <-h.publish:
			for c := range h.clients[p.communityID] {
				select {
				case c.send <- p.data:
				default:
					h.remove(c)
				}
			}
		case r := <-h.revoke:
			for c := range h.clients[r.communityID] {
				if c.userID == r.userID {
					h.remove(c)
				}
			}
		case <-h.done:
			for _, group := range h.clients {
				for c := range group {
					h.remove(c)
				}
			}
			return
		}
	}
}

func (h *Hub) remove(c *Client) {
	group := h.clients[c.communityID]
	if group != nil && group[c] {
		delete(group, c)
		if c.closed.CompareAndSwap(false, true) {
			close(c.send)
			_ = c.conn.Close()
		}
		if len(group) == 0 {
			delete(h.clients, c.communityID)
		}
	}
}

func (h *Hub) Publish(e domain.RealtimeEvent) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	select {
	case h.publish <- publish{e.CommunityID, b}:
	default:
		h.log.Error("realtime publish queue full", "event", e.Type)
	}
}
func (h *Hub) Revoke(communityID, userID string) {
	select {
	case h.revoke <- revoke{communityID, userID}:
	default:
		h.log.Error("realtime revoke queue full", "community", communityID)
	}
}
func (h *Hub) Shutdown() {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

var upgrader = websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: func(r *http.Request) bool { return true }}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, userID, communityID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Client{hub: h, conn: conn, send: make(chan []byte, 32), userID: userID, communityID: communityID}
	h.register <- c
	go c.writePump()
	go c.readPump()
}

func (c *Client) readPump() {
	defer func() { c.hub.unregister <- c }()
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
