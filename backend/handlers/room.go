package handlers

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"scribble.io/database"
	"scribble.io/game"
	"scribble.io/middleware"
)

const (
	roomCodeLength   = 6
	maxCodeAttempts  = 8
	defaultRoundWord = "apple"
)

type JoinRoomRequest struct {
	RoomID string `json:"room_id"`
}

type StartGameRequest struct {
	RoomID string `json:"room_id"`
}

func JoinRoom(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)

	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req JoinRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	roomCode := strings.ToUpper(strings.TrimSpace(req.RoomID))

	if roomCode == "" {
		http.Error(w, "room_id is required", http.StatusBadRequest)
		return
	}

	tx, err := database.DB.Begin(r.Context())
	if err != nil {
		http.Error(w, "Could not process request", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var roomDBID string
	var hostID string
	var status string
	var maxPlayers int

	err = tx.QueryRow(
		r.Context(),
		`SELECT id::text, COALESCE(host_id::text, ''), status, max_players
		 FROM rooms
		 WHERE room_code = $1
		 FOR UPDATE`,
		roomCode,
	).Scan(&roomDBID, &hostID, &status, &maxPlayers)

	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Could not process request", http.StatusInternalServerError)
		return
	}

	if status != "waiting" {
		http.Error(w, "Game already started", http.StatusConflict)
		return
	}

	var alreadyMember bool
	err = tx.QueryRow(
		r.Context(),
		`SELECT EXISTS(
			SELECT 1 FROM room_players
			WHERE room_id = $1 AND user_id = $2
		)`,
		roomDBID,
		userID,
	).Scan(&alreadyMember)
	if err != nil {
		http.Error(w, "Could not process request", http.StatusInternalServerError)
		return
	}

	var playersCount int
	err = tx.QueryRow(
		r.Context(),
		`SELECT COUNT(*) FROM room_players WHERE room_id = $1`,
		roomDBID,
	).Scan(&playersCount)
	if err != nil {
		http.Error(w, "Could not process request", http.StatusInternalServerError)
		return
	}

	if !alreadyMember && playersCount >= maxPlayers {
		http.Error(w, "Room is full", http.StatusConflict)
		return
	}

	if !alreadyMember {
		_, err = tx.Exec(
			r.Context(),
			`INSERT INTO room_players (room_id, user_id, is_host)
			 VALUES ($1, $2, FALSE)`,
			roomDBID,
			userID,
		)
		if err != nil {
			http.Error(w, "Could not process request", http.StatusInternalServerError)
			return
		}
		playersCount++
	}

	if err = tx.Commit(r.Context()); err != nil {
		http.Error(w, "Could not process request", http.StatusInternalServerError)
		return
	}

	// Keep in-memory room hydrated for WS/event flow while DB is source of truth.
	game.Mutex.Lock()
	room, exists := game.Rooms[roomCode]
	if !exists {
		room = &game.Room{
			ID:      roomCode,
			HostID:  hostID,
			Players: make(map[string]bool),
		}
		game.Rooms[roomCode] = room
	}
	if room.Players == nil {
		room.Players = make(map[string]bool)
	}
	room.Players[userID] = true
	room.IsStarted = status != "waiting"
	game.Mutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       "Joined room",
		"room_id":       roomCode,
		"host_id":       hostID,
		"players_count": playersCount,
		"is_started":    status != "waiting",
	})
}

func generateRoomCode() (string, error) {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	randomBytes := make([]byte, roomCodeLength)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	code := make([]byte, roomCodeLength)
	for i := range randomBytes {
		code[i] = letters[int(randomBytes[i])%len(letters)]
	}

	return string(code), nil
}

func CreateRoom(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)

	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	for attempt := 0; attempt < maxCodeAttempts; attempt++ {
		roomCode, err := generateRoomCode()
		if err != nil {
			http.Error(w, "Could not create room", http.StatusInternalServerError)
			return
		}

		tx, err := database.DB.Begin(r.Context())
		if err != nil {
			http.Error(w, "Could not create room", http.StatusInternalServerError)
			return
		}

		var roomDBID string
		err = tx.QueryRow(
			r.Context(),
			`INSERT INTO rooms (room_code, host_id, status)
			 VALUES ($1, $2, 'waiting')
			 RETURNING id::text`,
			roomCode,
			userID,
		).Scan(&roomDBID)
		if err != nil {
			tx.Rollback(r.Context())
			if isRoomCodeConflict(err) {
				continue
			}
			http.Error(w, "Could not create room", http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec(
			r.Context(),
			`INSERT INTO room_players (room_id, user_id, is_host)
			 VALUES ($1, $2, TRUE)`,
			roomDBID,
			userID,
		)
		if err != nil {
			tx.Rollback(r.Context())
			http.Error(w, "Could not create room", http.StatusInternalServerError)
			return
		}

		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, "Could not create room", http.StatusInternalServerError)
			return
		}

		game.Mutex.Lock()
		game.Rooms[roomCode] = &game.Room{
			ID:      roomCode,
			HostID:  userID,
			Players: map[string]bool{userID: true},
		}
		game.Mutex.Unlock()

		response := map[string]string{
			"room_id": roomCode,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	http.Error(w, "Could not create room", http.StatusInternalServerError)
}

func StartGame(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)

	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req StartGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid requst body", http.StatusBadRequest)
		return
	}

	roomCode := strings.ToUpper(strings.TrimSpace(req.RoomID))
	if roomCode == "" {
		http.Error(w, "room_id is required", http.StatusBadRequest)
		return
	}

	tx, err := database.DB.Begin(r.Context())

	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	defer tx.Rollback(r.Context())
	var roomDBID string
	var hostID string
	var status string

	err = tx.QueryRow(
		r.Context(),
		`SELECT id::text, COALESCE(host_id::text, ''), status
		 FROM rooms
		 WHERE room_code = $1
		 FOR UPDATE`,
		roomCode,
	).Scan(&roomDBID, &hostID, &status)

	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	if hostID != userID {
		http.Error(w, "Only host can start game", http.StatusForbidden)
		return
	}
	if status != "waiting" {
		http.Error(w, "Game already started", http.StatusConflict)
		return
	}

	var playersCount int
	err = tx.QueryRow(
		r.Context(),
		`SELECT COUNT(*) FROM room_players WHERE room_id = $1`,
		roomDBID,
	).Scan(&playersCount)
	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	if playersCount < 2 {
		http.Error(w, "Need at least 2 players to start", http.StatusConflict)
		return
	}

	playerIDs, err := loadRoomPlayerIDs(r.Context(), tx, roomDBID)
	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}
	if len(playerIDs) < 2 {
		http.Error(w, "Need at least 2 players to start", http.StatusConflict)
		return
	}

	drawerID := playerIDs[0]
	word, err := pickRoundWord(r.Context(), tx)
	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	var gameID string
	err = tx.QueryRow(
		r.Context(),
		`INSERT INTO games (room_id, started_at)
		 VALUES ($1, NOW())
		 RETURNING id::text`,
		roomDBID,
	).Scan(&gameID)
	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(
		r.Context(),
		`UPDATE rooms SET status = 'playing' WHERE id = $1`,
		roomDBID,
	)
	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	scoreMap := make(map[string]int, len(playerIDs))
	for _, pid := range playerIDs {
		_, err = tx.Exec(
			r.Context(),
			`INSERT INTO scores (game_id, user_id, points)
			 VALUES ($1, $2, 0)
			 ON CONFLICT (game_id, user_id) DO NOTHING`,
			gameID,
			pid,
		)
		if err != nil {
			http.Error(w, "Could not start game", http.StatusInternalServerError)
			return
		}
		scoreMap[pid] = 0
	}

	var roundID string
	err = tx.QueryRow(
		r.Context(),
		`INSERT INTO rounds (game_id, drawer_id, word, round_number, started_at)
		 VALUES ($1, $2, $3, 1, NOW())
		 RETURNING id::text`,
		gameID,
		drawerID,
		word,
	).Scan(&roundID)
	if err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		http.Error(w, "Could not start game", http.StatusInternalServerError)
		return
	}

	game.Mutex.Lock()
	room, exists := game.Rooms[roomCode]
	if !exists {
		room = &game.Room{
			ID:      roomCode,
			HostID:  hostID,
			Players: make(map[string]bool),
		}
		game.Rooms[roomCode] = room
	}
	room.IsStarted = true
	game.Mutex.Unlock()

	game.SetRuntimeState(roomCode, &game.RuntimeState{
		GameID:        gameID,
		RoundID:       roundID,
		RoundNumber:   1,
		DrawerID:      drawerID,
		Word:          word,
		PlayerOrder:   playerIDs,
		Scores:        scoreMap,
		GuessedUsers:  make(map[string]bool),
		IsRoundActive: true,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       "Game started",
		"room_id":       roomCode,
		"game_id":       gameID,
		"round_id":      roundID,
		"drawer_id":     drawerID,
		"status":        "playing",
		"players_count": playersCount,
	})
}

func isRoomCodeConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "room_code")
}

func loadRoomPlayerIDs(ctx context.Context, tx pgx.Tx, roomDBID string) ([]string, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT user_id::text
		 FROM room_players
		 WHERE room_id = $1
		 ORDER BY joined_at ASC`,
		roomDBID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var playerIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		playerIDs = append(playerIDs, userID)
	}

	return playerIDs, rows.Err()
}

func pickRoundWord(ctx context.Context, tx pgx.Tx) (string, error) {
	var word string
	err := tx.QueryRow(ctx, `SELECT word FROM words ORDER BY RANDOM() LIMIT 1`).Scan(&word)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultRoundWord, nil
	}
	if err != nil {
		return "", err
	}

	word = strings.TrimSpace(word)
	if word == "" {
		return defaultRoundWord, nil
	}
	return word, nil
}
