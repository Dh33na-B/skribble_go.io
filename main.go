package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn     *websocket.Conn
	hub      *Hub
	send     chan []byte
	username string
}

type GameState struct {
	WordLength int            `json:"word_length"`
	Scores     map[string]int `json:"scores"`
	History    []string       `json:"history"`
	TimeLeft   int            `json:"time_left"`
}

type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan string
	tick       chan bool

	targetWord string
	scores     map[string]int
	history    []string

	roundTime int
	timeLeft  int
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func generateWord() string {
	words := []string{"apple", "banana", "gopher", "dog", "cat"}
	return words[rand.Intn(len(words))]
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan string),
		tick:       make(chan bool),
		targetWord: generateWord(),
		scores:     make(map[string]int),
		history:    []string{},
		roundTime:  30,
		timeLeft:   30,
	}
}

func (h *Hub) run() {
	for {
		select {

		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			delete(h.clients, client)
			close(client.send)

		case msg := <-h.broadcast:
			h.history = append(h.history, msg)
			if len(h.history) > 100 {
				h.history = h.history[1:]
			}
			for client := range h.clients {
				select {
				case client.send <- []byte(msg):
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}

		case <-h.tick:
			h.timeLeft--

			if h.timeLeft <= 0 {
				h.history = append(h.history, " Time's up! New word generated.")
				h.targetWord = generateWord()
				h.timeLeft = h.roundTime
			}

			// Broadcast updated state every second
			for client := range h.clients {
				h.sendFullState(client)
			}
		}
	}
}

func (h *Hub) sendFullState(c *Client) {
	state := GameState{
		WordLength: len(h.targetWord),
		Scores:     h.scores,
		History:    h.history,
		TimeLeft:   h.timeLeft,
	}
	data, _ := json.Marshal(state)
	c.send <- data
}

func (h *Hub) startTimer() {
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for range ticker.C {
			h.tick <- true
		}
	}()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		msgStr := string(message)

		if c.username == "" {
			c.username = msgStr
			if _, exists := c.hub.scores[c.username]; !exists {
				c.hub.scores[c.username] = 0
			}
			c.hub.sendFullState(c)
			continue
		}

		if strings.ToLower(msgStr) == c.hub.targetWord {
			c.hub.scores[c.username]++
			c.hub.history = append(c.hub.history,
				" "+c.username+" guessed the word!")

			c.hub.targetWord = generateWord()
			c.hub.timeLeft = c.hub.roundTime
		} else {
			c.hub.broadcast <- c.username + ": " + msgStr
		}
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()
	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			return
		}
	}
}

func serveWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &Client{
		conn: conn,
		hub:  hub,
		send: make(chan []byte, 256),
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}

func main() {
	rand.Seed(time.Now().UnixNano())

	hub := newHub()
	go hub.run()
	hub.startTimer()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, w, r)
	})

	log.Println("Game server running on :42069")
	log.Fatal(http.ListenAndServe(":42069", nil))
}
