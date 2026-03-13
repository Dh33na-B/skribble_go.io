package game

import "sync"

type Hub struct {
	RoomCode   string
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
}

type Client struct {
	UserID string
	Hub    *Hub
	Send   chan []byte
}

var (
	hubsMutex sync.Mutex
	hubs      = make(map[string]*Hub)
)

func GetOrCreateHub(roomCode string) *Hub {
	hubsMutex.Lock()
	defer hubsMutex.Unlock()

	if hub, ok := hubs[roomCode]; ok {
		return hub
	}

	hub := &Hub{
		RoomCode:   roomCode,
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
	}
	hubs[roomCode] = hub

	go hub.Run()
	return hub
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			if len(h.clients) == 0 {
				hubsMutex.Lock()
				delete(hubs, h.RoomCode)
				hubsMutex.Unlock()
				return
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					delete(h.clients, client)
					close(client.Send)
				}
			}
		}
	}
}
