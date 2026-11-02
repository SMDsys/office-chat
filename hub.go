package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type wsClient struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
	online  map[string]int
	store   *Store
	log     *log.Logger
	up      websocket.Upgrader
}

func NewHub(store *Store, logger *log.Logger) *Hub {
	return &Hub{
		clients: map[*wsClient]struct{}{},
		online:  map[string]int{},
		store:   store,
		log:     logger,
		up: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				host := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
				return strings.EqualFold(host, r.Host)
			},
		},
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, userID string) {
	conn, err := h.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsClient{userID: userID, conn: conn, send: make(chan []byte, 128)}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.online[userID]++
	first := h.online[userID] == 1
	online := h.onlineListLocked()
	h.mu.Unlock()

	go h.writeLoop(c)
	h.sendJSON(c, Event{Type: "hello", Data: map[string]any{
		"online":     online,
		"serverTime": time.Now(),
	}})
	if first {
		h.store.TouchUser(userID)
		h.Broadcast(Event{Type: "presence", Data: map[string]any{
			"userId": userID,
			"online": true,
		}})
	}
	h.readLoop(c)
}

func (h *Hub) readLoop(c *wsClient) {
	defer h.unregister(c)
	c.conn.SetReadLimit(8192)
	_ = c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) writeLoop(c *wsClient) {
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case b, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
				h.unregister(c)
				return
			}
		case <-ping.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.unregister(c)
				return
			}
		}
	}
}

func (h *Hub) unregister(c *wsClient) {
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c)
	close(c.send)
	h.online[c.userID]--
	last := h.online[c.userID] <= 0
	if last {
		delete(h.online, c.userID)
	}
	h.mu.Unlock()
	_ = c.conn.Close()
	if last {
		h.store.TouchUser(c.userID)
		h.Broadcast(Event{Type: "presence", Data: map[string]any{
			"userId": c.userID,
			"online": false,
		}})
	}
}

func (h *Hub) sendJSON(c *wsClient, ev Event) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	default:
	}
}

// Publish отправляет событие всем, для кого allow возвращает true (nil — всем).
func (h *Hub) Publish(ev Event, allow func(userID string) bool) {
	b, err := json.Marshal(ev)
	if err != nil {
		h.log.Printf("ws: %v", err)
		return
	}
	h.mu.RLock()
	for c := range h.clients {
		if allow == nil || allow(c.userID) {
			select {
			case c.send <- b:
			default:
				go h.unregister(c)
			}
		}
	}
	h.mu.RUnlock()
}

func (h *Hub) Broadcast(ev Event) { h.Publish(ev, nil) }

func (h *Hub) IsOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.online[userID] > 0
}

func (h *Hub) OnlineUsers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.onlineListLocked()
}

func (h *Hub) onlineListLocked() []string {
	out := make([]string, 0, len(h.online))
	for id := range h.online {
		out = append(out, id)
	}
	return out
}

func (h *Hub) Connections() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
