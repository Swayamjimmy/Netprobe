package ws

import (
	"sync"

	"github.com/coder/websocket"
)

// Message represents a WebSocket message sent to clients
type Message struct {
	Type   string      `json:"type"`
	Target string      `json:"target,omitempty"`
	Data   interface{} `json:"data"`
}

// Client wraps a single WebSocket connection
type Client struct {
	conn *websocket.Conn
	send chan Message
}

// Hub manages all active WebSocket clients and broadcasts messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates and returns a fresh Hub instance
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}
