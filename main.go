package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Operation represents a collaborative editing operation
type Operation struct {
	Type     string `json:"type"`
	Position int    `json:"position"`
	Text     string `json:"text,omitempty"`
	Length   int    `json:"length,omitempty"`
	SelectionStart int `json:"selectionStart,omitempty"`
	SelectionEnd   int `json:"selectionEnd,omitempty"`
	Version  int    `json:"version"`
	ClientID string `json:"clientId"`
}

// Document represents the shared document state
type Document struct {
	Content string      `json:"content"`
	Version int         `json:"version"`
	mutex   sync.RWMutex
}

// Client represents a connected client
type Client struct {
	ID     string
	Conn   *websocket.Conn
	Send   chan Operation
	Doc    *Document
}

// Hub maintains the set of active clients and broadcasts operations
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Operation
	register   chan *Client
	unregister chan *Client
	document   *Document
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connections from any origin
	},
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan Operation),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		document: &Document{
			Content: "# Welcome to the Collaborative Editor\n\nStart typing to see real-time collaboration in action!\n\n- Multiple users can edit simultaneously\n- Changes are synchronized across all connected clients\n- Line numbers are displayed for easy reference\n\nTry opening this page in multiple tabs or browsers to test the collaboration features.",
			Version: 0,
		},
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			// Send current document state to new client
			h.document.mutex.RLock()
			initOp := Operation{
				Type:    "init",
				Text:    h.document.Content,
				Version: h.document.Version,
			}
			h.document.mutex.RUnlock()
			
			select {
			case client.Send <- initOp:
			default:
				close(client.Send)
				delete(h.clients, client)
			}
			log.Printf("Client %s connected. Total clients: %d", client.ID, len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
				log.Printf("Client %s disconnected. Total clients: %d", client.ID, len(h.clients))
			}

		case operation := <-h.broadcast:
			// Apply operation to document
			h.applyOperation(operation)
			
			// Broadcast to all clients except sender
			for client := range h.clients {
				if client.ID != operation.ClientID {
					select {
					case client.Send <- operation:
					default:
						close(client.Send)
						delete(h.clients, client)
					}
				}
			}
		}
	}
}

func (h *Hub) applyOperation(op Operation) {
	h.document.mutex.Lock()
	defer h.document.mutex.Unlock()

	switch op.Type {
	case "insert":
		if op.Position <= len(h.document.Content) {
			h.document.Content = h.document.Content[:op.Position] + op.Text + h.document.Content[op.Position:]
			h.document.Version++
		}
	case "delete":
		if op.Position >= 0 && op.Position+op.Length <= len(h.document.Content) {
			h.document.Content = h.document.Content[:op.Position] + h.document.Content[op.Position+op.Length:]
			h.document.Version++
		}
	}
}

func (c *Client) writePump() {
	defer c.Conn.Close()
	
	for {
		select {
		case operation, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			
			if err := c.Conn.WriteJSON(operation); err != nil {
				log.Printf("Error writing to client %s: %v", c.ID, err)
				return
			}
		}
	}
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.Conn.Close()
	}()

	for {
		var operation Operation
		err := c.Conn.ReadJSON(&operation)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Error reading from client %s: %v", c.ID, err)
			}
			break
		}

		operation.ClientID = c.ID
		hub.broadcast <- operation
	}
}

func serveWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientID := r.URL.Query().Get("clientId")
	if clientID == "" {
		clientID = fmt.Sprintf("client_%d", len(hub.clients)+1)
	}

	client := &Client{
		ID:   clientID,
		Conn: conn,
		Send: make(chan Operation, 256),
		Doc:  hub.document,
	}

	hub.register <- client

	go client.writePump()
	go client.readPump(hub)
}

func main() {
	hub := newHub()
	go hub.run()

	// Serve static files
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "index.html")
		} else {
			http.NotFound(w, r)
		}
	})

	// WebSocket endpoint
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, w, r)
	})

	// API endpoint to get document state
	http.HandleFunc("/api/document", func(w http.ResponseWriter, r *http.Request) {
		hub.document.mutex.RLock()
		doc := map[string]interface{}{
			"content": hub.document.Content,
			"version": hub.document.Version,
		}
		hub.document.mutex.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doc)
	})

	port := "8080"
	log.Printf("Collaborative editor server starting on port %s", port)
	log.Printf("Open http://localhost:%s in your browser", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}