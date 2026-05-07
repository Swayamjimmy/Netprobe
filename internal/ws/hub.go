package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Message struct {
	Type   string      `json:"type"`
	Target string      `json:"target,omitempty"`
	Data   interface{} `json:"data"`
}

type Client struct {
	conn *websocket.Conn
	send chan Message
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(msg Message) {
	h.broadcast <- msg
}

func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		log.Printf("WebSocket accept error: %v", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan Message, 64),
	}
	h.register <- client

	go func() {
		defer func() {
			h.unregister <- client
			conn.CloseNow()
		}()
		for msg := range client.send {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := wsjson.Write(ctx, conn, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}()

	for {
		var incoming map[string]interface{}
		err := wsjson.Read(context.Background(), conn, &incoming)
		if err != nil {
			return
		}
		h.handleClientMessage(incoming, client)
	}
}

func (h *Hub) handleClientMessage(msg map[string]interface{}, client *Client) {
	data, _ := json.Marshal(msg)
	log.Printf("Received from client: %s", string(data))
}
