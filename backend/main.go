package main

import (
	"log"
	"net/http"

	"scribble.io/database"
	"scribble.io/routes"
)

func main() {

	// Connect DB
	database.ConnectDB()


	router := routes.SetupRoutes()

	log.Println(" Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}