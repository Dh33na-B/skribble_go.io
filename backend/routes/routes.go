package routes

import (
	"net/http"

	"scribble.io/handlers"
)

func SetupRoutes () http.Handler{

	mux := http.NewServeMux() 

	mux.HandleFunc("/register",handlers.RegisterUser) 
	mux.HandleFunc("/login", handlers.Login)



	return mux
}