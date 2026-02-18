package main

import (
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
)


type Client struct{
	conn *websocket.Conn 
	hub *Hub 
	send chan []byte
	username string 
	points int  
}

type Hub struct{
	clients map[*Client]bool
	register chan *Client 
	unregister chan *Client 
	broadcast chan []byte  
	targetWord string 
}

// converts http to websocket connection by keeyping raw TCP open 
var upgrader = websocket.Upgrader{

	// allows all origin 
	CheckOrigin: func (r* http.Request) bool{
		return true
	},
}

func newHub() *Hub{
	return &Hub{
		clients: make(map[*Client]bool),
		register: make(chan *Client),
		unregister: make(chan *Client),
		broadcast: make(chan []byte),
		targetWord: generateWord(),
	}
}

func generateWord() string {
	words := []string{"apple","banana","cat","dog","gopher"}

	return words[rand.Intn(len(words))]
}

func (h* Hub) run(){
	for{
		select{
		case client := <-h.register:
			h.clients[client] = true
			client.send <- []byte("welcome! current word length: "+strconv.Itoa(len(h.targetWord)))
			log.Println("client registered")
		
		case client := <-h.unregister:
			if _,ok := h.clients[client];ok{
				delete(h.clients,client)
				close(client.send)
				log.Println("client removed")
			}
		case message := <-h.broadcast:
			for client := range h.clients{
				select{
				case client.send <- message:
				default:
					// slow client -> removal
					close(client.send)
					delete(h.clients,client)
				}
			}
		}
	}
}

func (c *Client) readPump(){
	defer func(){
		c.hub.unregister <- c 
		c.conn.Close()
	}()

	for{
		_,message,err := c.conn.ReadMessage()
		if err != nil{
			log.Println("error in reading from socket",err)
			break
		}

		msgstr := string(message)

		// First message is username 
		if c.username == ""{
			c.username = msgstr 
			c.send <- []byte("username set to: "+c.username)
			continue
		}

		log.Println(c.username,"guessed:",msgstr) 

		// check correct word 
		if strings.ToLower(msgstr) == c.hub.targetWord{
			c.points++ 

			winmsg := c.username + " guessed the word! Points: "+ strconv.Itoa(c.points)

			c.hub.broadcast <- []byte(winmsg)

			// Generate new word 

			c.hub.targetWord = generateWord() 

			c.hub.broadcast <- []byte("new word length: " + strconv.Itoa(len(c.hub.targetWord)))
			continue
		}
		c.hub.broadcast <- []byte(c.username + ":"+msgstr)
	}
}

func (c *Client) writePump(){
	defer c.conn.Close()

	for message := range c.send{
		err := c.conn.WriteMessage(websocket.TextMessage,message)
		if err != nil{
			log.Println("error in writing from socket",err)	
			return 	
		}
	}
}

func serveWS(hub *Hub,w http.ResponseWriter,r *http.Request){
	conn, err := upgrader.Upgrade(w,r,nil)
	if err != nil{
		log.Println("error in upgrading to websocket")
		return 
	}

	client := &Client{
		conn: conn,
		hub: hub,
		send: make(chan []byte,256),
	}

	client.hub.register <- client 

	go client.writePump()
	go client.readPump()
}

func main(){

	hub := newHub()

	go hub.run() 

	http.HandleFunc("/ws",func(w http.ResponseWriter, r* http.Request){
		serveWS(hub,w,r)
	})


	log.Println("skribbl-style server started on :42069")

	log.Fatal(http.ListenAndServe(":42069",nil))
}