package routes

import (
	"net/http"

	"scribble.io/handlers"
	"scribble.io/middleware"
)

func SetupRoutes() http.Handler {

	mux := http.NewServeMux()

	mux.HandleFunc("/login", handlers.Login)

	mux.HandleFunc("/register", handlers.RegisterUser)

	protectedCreateRoom := middleware.AuthMiddleware(
		http.HandlerFunc(handlers.CreateRoom),
	)
	protectedJoinRoom := middleware.AuthMiddleware(
		http.HandlerFunc(handlers.JoinRoom),
	)
	protectedStartGame := middleware.AuthMiddleware(
		http.HandlerFunc(handlers.StartGame),
	)
	mux.HandleFunc("/ws", handlers.ServeWS)

	mux.Handle("/start-game", protectedStartGame)
	mux.Handle("/join-room", protectedJoinRoom)
	mux.Handle("/create-room", protectedCreateRoom)

	return withCORS(mux)
}

func Profile(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.UserIDKey).(string)

	w.Write([]byte("User ID: " + userID))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
