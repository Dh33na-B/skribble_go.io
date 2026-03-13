package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"scribble.io/database"
	"scribble.io/game"
	"scribble.io/utils"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 8 * 1024

	EventChatMessage  = "chat_message"
	EventDrawStroke   = "draw_stroke"
	EventGuessSubmit  = "guess_submit"
	EventGuessResult  = "guess_result"
	EventScoreUpdate  = "score_update"
	EventRoundEnd     = "round_end"
	EventStateSnap    = "state_snapshot"
	EventPlayerJoined = "player_joined"
	EventPlayerLeft   = "player_left"
	EventError        = "error"

	maxChatChars  = 300
	maxGuessChars = 100
	maxDrawSize   = 50.0

	correctGuessPoints = 100
	drawerBonusPoints  = 20
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var allowedClientEvents = map[string]struct{}{
	EventChatMessage: {},
	EventDrawStroke:  {},
	EventGuessSubmit: {},
}

type wsInboundMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type wsOutboundMessage struct {
	Type    string          `json:"type"`
	RoomID  string          `json:"room_id"`
	UserID  string          `json:"user_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	TS      int64           `json:"ts"`
}

type chatMessagePayload struct {
	Text string `json:"text"`
}

type drawStrokePayload struct {
	X0    float64 `json:"x0"`
	Y0    float64 `json:"y0"`
	X1    float64 `json:"x1"`
	Y1    float64 `json:"y1"`
	Color string  `json:"color"`
	Size  float64 `json:"size"`
}

type guessSubmitPayload struct {
	Text string `json:"text"`
}

func ServeWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomCode := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("room_id")))
	if roomCode == "" {
		http.Error(w, "room_id is required", http.StatusBadRequest)
		return
	}

	token := extractToken(r)
	if token == "" {
		http.Error(w, "Missing token", http.StatusUnauthorized)
		return
	}

	claims, err := utils.ValidateJWT(token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	isMember, err := isRoomMember(r.Context(), roomCode, claims.UserID)
	if err != nil {
		http.Error(w, "Could not validate membership", http.StatusInternalServerError)
		return
	}
	if !isMember {
		http.Error(w, "Not a member of this room", http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	hub := game.GetOrCreateHub(roomCode)
	client := &game.Client{
		UserID: claims.UserID,
		Hub:    hub,
		Send:   make(chan []byte, 256),
	}

	hub.Register(client)
	sendStateSnapshot(client, roomCode)
	broadcastPresence(hub, EventPlayerJoined, roomCode, claims.UserID)

	go writePump(conn, client)
	readPump(conn, client, roomCode)
}

func readPump(conn *websocket.Conn, client *game.Client, roomCode string) {
	defer func() {
		client.Hub.Unregister(client)
		broadcastPresence(client.Hub, EventPlayerLeft, roomCode, client.UserID)
		conn.Close()
	}()

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var in wsInboundMessage
		if err := json.Unmarshal(message, &in); err != nil {
			sendErrorEvent(client, roomCode, "Invalid message JSON")
			continue
		}

		in.Type = normalizeEventType(in.Type)
		if in.Type == "" {
			sendErrorEvent(client, roomCode, "Missing event type")
			continue
		}

		if _, ok := allowedClientEvents[in.Type]; !ok {
			sendErrorEvent(client, roomCode, "Unsupported event type")
			continue
		}

		events, err := dispatchClientEvent(client, roomCode, in)
		if err != nil {
			sendErrorEvent(client, roomCode, err.Error())
			continue
		}

		for _, event := range events {
			client.Hub.Broadcast(event)
		}
	}
}

func writePump(conn *websocket.Conn, client *game.Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			writer, err := conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			if _, err := writer.Write(message); err != nil {
				writer.Close()
				return
			}

			n := len(client.Send)
			for i := 0; i < n; i++ {
				if _, err := writer.Write([]byte{'\n'}); err != nil {
					writer.Close()
					return
				}
				if _, err := writer.Write(<-client.Send); err != nil {
					writer.Close()
					return
				}
			}

			if err := writer.Close(); err != nil {
				return
			}

		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func broadcastPresence(hub *game.Hub, eventType, roomCode, userID string) {
	msg, err := buildOutboundEvent(eventType, roomCode, userID, nil)
	if err != nil {
		return
	}
	hub.Broadcast(msg)
}

func dispatchClientEvent(client *game.Client, roomCode string, in wsInboundMessage) ([][]byte, error) {
	switch in.Type {
	case EventChatMessage:
		var payload chatMessagePayload
		if err := json.Unmarshal(in.Payload, &payload); err != nil {
			return nil, fmt.Errorf("Invalid chat payload")
		}

		text, err := normalizeText(payload.Text, maxChatChars)
		if err != nil {
			return nil, fmt.Errorf("Invalid chat message")
		}
		payload.Text = text

		event, err := buildOutboundEvent(EventChatMessage, roomCode, client.UserID, payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{event}, nil

	case EventDrawStroke:
		if err := ensureCanDraw(roomCode, client.UserID); err != nil {
			return nil, err
		}

		var payload drawStrokePayload
		if err := json.Unmarshal(in.Payload, &payload); err != nil {
			return nil, fmt.Errorf("Invalid draw payload")
		}

		if !isFinite(payload.X0) || !isFinite(payload.Y0) || !isFinite(payload.X1) || !isFinite(payload.Y1) {
			return nil, fmt.Errorf("Invalid draw coordinates")
		}

		if !isFinite(payload.Size) || payload.Size <= 0 || payload.Size > maxDrawSize {
			return nil, fmt.Errorf("Invalid draw size")
		}

		payload.Color = strings.TrimSpace(payload.Color)
		if !isValidHexColor(payload.Color) {
			return nil, fmt.Errorf("Invalid draw color")
		}

		event, err := buildOutboundEvent(EventDrawStroke, roomCode, client.UserID, payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{event}, nil

	case EventGuessSubmit:
		return handleGuessSubmit(client, roomCode, in.Payload)
	}

	return nil, fmt.Errorf("Unsupported event type")
}

func handleGuessSubmit(client *game.Client, roomCode string, rawPayload json.RawMessage) ([][]byte, error) {
	var payload guessSubmitPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, fmt.Errorf("Invalid guess payload")
	}

	text, err := normalizeText(payload.Text, maxGuessChars)
	if err != nil {
		return nil, fmt.Errorf("Invalid guess message")
	}
	payload.Text = text

	var (
		gameID         string
		roundID        string
		drawerID       string
		word           string
		alreadyGuessed bool
		roundActive    bool
		roundDone      bool
		reserved       bool
	)

	ok := game.WithRuntimeState(roomCode, func(state *game.RuntimeState) {
		gameID = state.GameID
		roundID = state.RoundID
		drawerID = state.DrawerID
		word = state.Word
		roundActive = state.IsRoundActive
		alreadyGuessed = state.GuessedUsers[client.UserID]
	})
	if !ok {
		return nil, fmt.Errorf("Game state not initialized")
	}

	if !roundActive {
		return nil, fmt.Errorf("Round is not active")
	}
	if drawerID == client.UserID {
		return nil, fmt.Errorf("Drawer cannot submit guesses")
	}
	if alreadyGuessed {
		return nil, fmt.Errorf("You already guessed correctly")
	}

	isCorrect := normalizeGuess(text) == normalizeGuess(word)
	if isCorrect {
		ok = game.WithRuntimeState(roomCode, func(state *game.RuntimeState) {
			// Recheck under lock to prevent duplicate scoring race.
			if !state.IsRoundActive || state.RoundID != roundID || state.GuessedUsers[client.UserID] {
				return
			}
			state.GuessedUsers[client.UserID] = true
			roundDone = len(state.GuessedUsers) >= countGuessTargets(state.PlayerOrder, state.DrawerID)
			if roundDone {
				state.IsRoundActive = false
			}
			reserved = true
		})
		if !ok || !reserved {
			return nil, fmt.Errorf("Guess already processed")
		}
	}

	ctx := context.Background()
	guesserScore, drawerScore, err := persistGuessOutcome(
		ctx,
		gameID,
		roundID,
		drawerID,
		client.UserID,
		text,
		isCorrect,
		roundDone,
	)
	if err != nil {
		if isCorrect && reserved {
			game.WithRuntimeState(roomCode, func(state *game.RuntimeState) {
				delete(state.GuessedUsers, client.UserID)
				if len(state.GuessedUsers) < countGuessTargets(state.PlayerOrder, state.DrawerID) {
					state.IsRoundActive = true
				}
			})
		}
		return nil, fmt.Errorf("Could not save guess")
	}

	events := make([][]byte, 0, 4)

	guessEvent, err := buildOutboundEvent(EventGuessSubmit, roomCode, client.UserID, payload)
	if err == nil {
		events = append(events, guessEvent)
	}

	resultEvent, err := buildOutboundEvent(EventGuessResult, roomCode, "", map[string]interface{}{
		"user_id":    client.UserID,
		"guess":      text,
		"is_correct": isCorrect,
	})
	if err == nil {
		events = append(events, resultEvent)
	}

	if !isCorrect {
		return events, nil
	}

	var scoresCopy map[string]int
	game.WithRuntimeState(roomCode, func(state *game.RuntimeState) {
		state.Scores[client.UserID] = guesserScore
		if drawerID != "" {
			state.Scores[drawerID] = drawerScore
		}

		scoresCopy = make(map[string]int, len(state.Scores))
		for userID, score := range state.Scores {
			scoresCopy[userID] = score
		}
	})

	scoreEvent, err := buildOutboundEvent(EventScoreUpdate, roomCode, "", map[string]interface{}{
		"scores": scoresCopy,
	})
	if err == nil {
		events = append(events, scoreEvent)
	}

	if roundDone {
		roundEndEvent, err := buildOutboundEvent(EventRoundEnd, roomCode, "", map[string]interface{}{
			"round_id": roundID,
			"word":     word,
		})
		if err == nil {
			events = append(events, roundEndEvent)
		}
	}

	return events, nil
}

func ensureCanDraw(roomCode, userID string) error {
	ok := false
	var drawAllowed bool
	var active bool

	ok = game.WithRuntimeState(roomCode, func(state *game.RuntimeState) {
		active = state.IsRoundActive
		drawAllowed = state.DrawerID == userID
	})
	if !ok {
		return fmt.Errorf("Game state not initialized")
	}

	if !active {
		return fmt.Errorf("Round is not active")
	}
	if !drawAllowed {
		return fmt.Errorf("Only current drawer can draw")
	}

	return nil
}

func persistGuessOutcome(
	ctx context.Context,
	gameID string,
	roundID string,
	drawerID string,
	userID string,
	guess string,
	isCorrect bool,
	roundDone bool,
) (int, int, error) {
	tx, err := database.DB.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(
		ctx,
		`INSERT INTO guesses (round_id, user_id, guessed_word, is_correct)
		 VALUES ($1, $2, $3, $4)`,
		roundID,
		userID,
		guess,
		isCorrect,
	)
	if err != nil {
		return 0, 0, err
	}

	guesserScore := 0
	drawerScore := 0

	if isCorrect {
		guesserScore, err = addScoreTx(ctx, tx, gameID, userID, correctGuessPoints)
		if err != nil {
			return 0, 0, err
		}

		if drawerID != "" && drawerID != userID {
			drawerScore, err = addScoreTx(ctx, tx, gameID, drawerID, drawerBonusPoints)
			if err != nil {
				return 0, 0, err
			}
		}
	}

	if roundDone {
		_, err = tx.Exec(
			ctx,
			`UPDATE rounds SET ended_at = NOW()
			 WHERE id = $1 AND ended_at IS NULL`,
			roundID,
		)
		if err != nil {
			return 0, 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}

	return guesserScore, drawerScore, nil
}

func addScoreTx(ctx context.Context, tx pgx.Tx, gameID string, userID string, points int) (int, error) {
	var total int
	err := tx.QueryRow(
		ctx,
		`INSERT INTO scores (game_id, user_id, points)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (game_id, user_id)
		 DO UPDATE SET points = scores.points + EXCLUDED.points
		 RETURNING points`,
		gameID,
		userID,
		points,
	).Scan(&total)

	return total, err
}

func sendStateSnapshot(client *game.Client, roomCode string) {
	payload, ok := buildRuntimeSnapshotPayload(roomCode, client.UserID)
	if !ok {
		return
	}

	event, err := buildOutboundEvent(EventStateSnap, roomCode, "", payload)
	if err != nil {
		return
	}

	select {
	case client.Send <- event:
	default:
	}
}

func buildRuntimeSnapshotPayload(roomCode, userID string) (map[string]interface{}, bool) {
	payload := map[string]interface{}{}

	ok := game.WithRuntimeState(roomCode, func(state *game.RuntimeState) {
		word := maskWordForViewer(state.Word, userID == state.DrawerID)

		scores := make(map[string]int, len(state.Scores))
		for uid, score := range state.Scores {
			scores[uid] = score
		}

		payload["game_id"] = state.GameID
		payload["round_id"] = state.RoundID
		payload["round_number"] = state.RoundNumber
		payload["drawer_id"] = state.DrawerID
		payload["word"] = word
		payload["is_round_active"] = state.IsRoundActive
		payload["players"] = append([]string(nil), state.PlayerOrder...)
		payload["scores"] = scores
	})

	return payload, ok
}

func buildOutboundEvent(eventType, roomCode, userID string, payload interface{}) ([]byte, error) {
	var rawPayload json.RawMessage
	if payload != nil {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		rawPayload = payloadBytes
	}

	out := wsOutboundMessage{
		Type:    eventType,
		RoomID:  roomCode,
		UserID:  userID,
		Payload: rawPayload,
		TS:      time.Now().Unix(),
	}

	return json.Marshal(out)
}

func sendErrorEvent(client *game.Client, roomCode, message string) {
	payload := map[string]string{
		"message": message,
	}

	event, err := buildOutboundEvent(EventError, roomCode, "", payload)
	if err != nil {
		return
	}

	select {
	case client.Send <- event:
	default:
	}
}

func normalizeEventType(eventType string) string {
	return strings.ToLower(strings.TrimSpace(eventType))
}

func normalizeText(text string, maxChars int) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("text is required")
	}

	if utf8.RuneCountInString(text) > maxChars {
		return "", fmt.Errorf("text too long")
	}

	return text, nil
}

func normalizeGuess(guess string) string {
	return strings.ToLower(strings.TrimSpace(guess))
}

func countGuessTargets(players []string, drawerID string) int {
	count := 0
	for _, userID := range players {
		if userID != drawerID {
			count++
		}
	}
	return count
}

func maskWordForViewer(word string, reveal bool) string {
	if reveal {
		return word
	}

	runes := []rune(word)
	for i := range runes {
		if runes[i] != ' ' {
			runes[i] = '_'
		}
	}
	return string(runes)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isValidHexColor(color string) bool {
	if len(color) != 4 && len(color) != 7 {
		return false
	}
	if color[0] != '#' {
		return false
	}

	for i := 1; i < len(color); i++ {
		c := color[i]
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		isUpperHex := c >= 'A' && c <= 'F'
		if !isDigit && !isLowerHex && !isUpperHex {
			return false
		}
	}
	return true
}

func isRoomMember(ctx context.Context, roomCode, userID string) (bool, error) {
	var exists bool
	err := database.DB.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM rooms r
			JOIN room_players rp ON rp.room_id = r.id
			WHERE r.room_code = $1 AND rp.user_id = $2
		)`,
		roomCode,
		userID,
	).Scan(&exists)

	return exists, err
}

func extractToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	if authHeader != "" {
		return authHeader
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}
