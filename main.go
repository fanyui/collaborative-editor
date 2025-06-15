package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/cors"
)

// Message types
const (
	MsgTypeJoin       = "join"
	MsgTypeOperation  = "operation"
	MsgTypeSelection  = "selection"
	MsgTypeCursor     = "cursor"
	MsgTypeWelcome    = "welcome"
	MsgTypeUserJoined = "userJoined"
	MsgTypeUserLeft   = "userLeft"
	MsgTypeUsers      = "users"
)

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for demo
	},
}

// Client represents a connected user
type Client struct {
	ID       string          `json:"clientId"`
	Username string          `json:"username"`
	Conn     *websocket.Conn `json:"-"`
	Hub      *Hub            `json:"-"`
	Send     chan []byte     `json:"-"`
	LastSeen time.Time       `json:"-"`
}

// Operation represents a document operation
type Operation struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ClientID  string `json:"clientId"`
	Operation struct {
		Retain int    `json:"retain"`
		Delete int    `json:"delete"`
		Insert string `json:"insert"`
	} `json:"operation"`
}

// SelectionMessage represents cursor/selection updates
type SelectionMessage struct {
	Type     string `json:"type"`
	ClientID string `json:"clientId"`
	From     int    `json:"from"`
	To       int    `json:"to"`
	Cursor   int    `json:"cursor"`
}

// Message represents any WebSocket message
type Message struct {
	Type      string      `json:"type"`
	ClientID  string      `json:"clientId,omitempty"`
	Username  string      `json:"username,omitempty"`
	Version   int         `json:"version,omitempty"`
	Document  string      `json:"document,omitempty"`
	Operation interface{} `json:"operation,omitempty"`
	From      int         `json:"from,omitempty"`
	To        int         `json:"to,omitempty"`
	Cursor    int         `json:"cursor,omitempty"`
	Users     []*Client   `json:"users,omitempty"`
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	document   string
	version    int
	operations []Operation
	mutex      sync.RWMutex
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		document:   "// Welcome to the Collaborative Editor!\n// Start typing to see real-time collaboration in action.\n\nfunction hello() {\n    console.log('Hello, collaborative world!');\n}\n\nhello();",
		version:    0,
		operations: make([]Operation, 0),
	}
}

// Run starts the hub
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true
			h.mutex.Unlock()

			// Send welcome message with current document
			welcomeMsg := Message{
				Type:     MsgTypeWelcome,
				ClientID: client.ID,
				Version:  h.version,
				Document: h.document,
			}

			if data, err := json.Marshal(welcomeMsg); err == nil {
				select {
				case client.Send <- data:
				default:
					close(client.Send)
					h.mutex.Lock()
					delete(h.clients, client)
					h.mutex.Unlock()
				}
			}

			// Notify other clients about new user
			userJoinedMsg := Message{
				Type:     MsgTypeUserJoined,
				ClientID: client.ID,
				Username: client.Username,
			}

			if data, err := json.Marshal(userJoinedMsg); err == nil {
				h.broadcastToOthers(data, client)
			}

			// Send current users list to new client
			h.sendUsersList(client)

			log.Printf("Client %s (%s) registered", client.ID, client.Username)

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)

				// Notify other clients about user leaving
				userLeftMsg := Message{
					Type:     MsgTypeUserLeft,
					ClientID: client.ID,
				}

				if data, err := json.Marshal(userLeftMsg); err == nil {
					h.broadcastToOthers(data, client)
				}

				log.Printf("Client %s (%s) unregistered", client.ID, client.Username)
			}
			h.mutex.Unlock()

		case message := <-h.broadcast:
			h.mutex.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
			h.mutex.RUnlock()
		}
	}
}

// broadcastToOthers sends a message to all clients except the sender
func (h *Hub) broadcastToOthers(message []byte, sender *Client) {
	h.mutex.RLock()
	clientCount := len(h.clients)
	h.mutex.RUnlock()

	log.Printf("Broadcasting message to %d total clients (excluding sender)", clientCount-1)

	h.mutex.RLock()
	defer h.mutex.RUnlock()

	sentCount := 0
	for client := range h.clients {
		if client != sender {
			select {
			case client.Send <- message:
				sentCount++
				log.Printf("Message sent to client %s (%s)", client.ID, client.Username)
			default:
				log.Printf("Failed to send message to client %s, closing connection", client.ID)
				close(client.Send)
				delete(h.clients, client)
			}
		}
	}

	log.Printf("Successfully sent message to %d clients", sentCount)
}

// sendUsersList sends the current users list to a specific client
func (h *Hub) sendUsersList(targetClient *Client) {
	h.mutex.RLock()
	users := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		users = append(users, &Client{
			ID:       client.ID,
			Username: client.Username,
		})
	}
	h.mutex.RUnlock()

	usersMsg := Message{
		Type:  MsgTypeUsers,
		Users: users,
	}

	if data, err := json.Marshal(usersMsg); err == nil {
		select {
		case targetClient.Send <- data:
		default:
			close(targetClient.Send)
			h.mutex.Lock()
			delete(h.clients, targetClient)
			h.mutex.Unlock()
		}
	}
}

// applyOperation applies an operation to the document
func (h *Hub) applyOperation(op Operation) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Simple operational transformation (basic implementation)
	retain := op.Operation.Retain
	deleteCount := op.Operation.Delete
	insert := op.Operation.Insert

	docRunes := []rune(h.document)

	// Apply the operation
	var result []rune

	// Retain characters
	if retain > 0 && retain <= len(docRunes) {
		result = append(result, docRunes[:retain]...)
	}

	// Insert new text
	if insert != "" {
		result = append(result, []rune(insert)...)
	}

	// Add remaining characters (after delete)
	if retain+deleteCount < len(docRunes) {
		result = append(result, docRunes[retain+deleteCount:]...)
	}

	h.document = string(result)
	h.version++
	h.operations = append(h.operations, op)

	log.Printf("Applied operation: retain=%d, delete=%d, insert='%s', new version=%d",
		retain, deleteCount, insert, h.version)
}

// readPump pumps messages from the websocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, messageBytes, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		msg.ClientID = c.ID
		c.handleMessage(msg)
	}
}

// writePump pumps messages from the hub to the websocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Send each message as a separate WebSocket message
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

			// Send any queued messages as separate WebSocket messages
			n := len(c.Send)
			for i := 0; i < n; i++ {
				queuedMessage := <-c.Send
				if err := c.Conn.WriteMessage(websocket.TextMessage, queuedMessage); err != nil {
					return
				}
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming messages from clients
func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case MsgTypeJoin:
		c.Username = msg.Username
		log.Printf("Client %s joined as %s", c.ID, c.Username)
		c.Hub.sendUsersList(c)

	case MsgTypeOperation:
		log.Printf("Received operation from client %s", c.ID)

		// Parse operation
		var op Operation
		opBytes, _ := json.Marshal(msg)
		if err := json.Unmarshal(opBytes, &op); err != nil {
			log.Printf("Error parsing operation: %v", err)
			return
		}

		op.ClientID = c.ID

		// Apply operation to document
		c.Hub.applyOperation(op)

		// Broadcast operation to other clients
		opMsg := Message{
			Type:      MsgTypeOperation,
			ClientID:  c.ID,
			Version:   op.Version,
			Operation: op.Operation,
		}

		if data, err := json.Marshal(opMsg); err == nil {
			log.Printf("Broadcasting operation to %d other clients", len(c.Hub.clients)-1)
			c.Hub.broadcastToOthers(data, c)
		}

	case MsgTypeSelection:
		log.Printf("Received selection from client %s: cursor=%d, from=%d, to=%d", c.ID, msg.Cursor, msg.From, msg.To)

		// Broadcast selection/cursor to other clients
		selectionMsg := Message{
			Type:     MsgTypeSelection,
			ClientID: c.ID,
			From:     msg.From,
			To:       msg.To,
			Cursor:   msg.Cursor,
		}

		if data, err := json.Marshal(selectionMsg); err == nil {
			log.Printf("Broadcasting selection to %d other clients", len(c.Hub.clients)-1)
			c.Hub.broadcastToOthers(data, c)
		}
	}
}

// generateClientID generates a unique client ID
func generateClientID() string {
	return fmt.Sprintf("client_%d", time.Now().UnixNano())
}

// serveWS handles websocket requests from clients
func serveWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		ID:       generateClientID(),
		Hub:      hub,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		LastSeen: time.Now(),
	}

	client.Hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in new goroutines
	go client.writePump()
	go client.readPump()
}

// serveHome serves the HTML file
func serveHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	hub := NewHub()
	go hub.Run()

	// Setup CORS
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHome)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, w, r)
	})

	handler := c.Handler(mux)

	log.Println("Collaborative editor server starting on :8080")
	log.Println("WebSocket endpoint: ws://localhost:8080/ws")

	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal("ListenAndServe error: ", err)
	}
}
