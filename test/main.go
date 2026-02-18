package test 

import (
	"fmt"
	"io"
	"net/http"

	"golang.org/x/net/websocket"
)

type Server struct {
	conns map[*websocket.Conn]bool
}


func NewServer() *Server{
	return &Server{
		conns:make(map[*websocket.Conn]bool),
	}
}

func (s *Server) handleWS(ws *websocket.Conn){
	fmt.Println("new incoming connection from client:",ws.RemoteAddr())

	s.conns[ws]=true

	s.readLoop(ws)
}

func (s *Server) readLoop(ws *websocket.Conn){
	buf := make([]byte,1024)
	for{
		n, err := ws.Read(buf)
		if err == io.EOF{
			break
		}
		if err != nil{
			fmt.Println("read error: ",err)
			continue
		}
		msg := buf[:n]

		s.broadcaste(msg)
	}

}

func (s *Server) broadcaste(b []byte){
	for ws:= range s.conns{
		go func(ws *websocket.Conn){
			if _,err := ws.Write(b); err != nil {
				fmt.Println("write error:",err)
			}
		}(ws)
	}
}

func main() {
	PORT := ":3000"

	fmt.Printf("serving running on port%s\n",PORT)
	server := NewServer()
	http.Handle("/ws",websocket.Handler(server.handleWS))
	err := http.ListenAndServe(":3000",nil)

	if err!= nil{
		fmt.Println("server error:",err)
	}
}
