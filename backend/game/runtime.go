package game

import "sync"

type RuntimeState struct {
	GameID        string
	RoundID       string
	RoundNumber   int
	DrawerID      string
	Word          string
	PlayerOrder   []string
	Scores        map[string]int
	GuessedUsers  map[string]bool
	IsRoundActive bool
}

type RuntimeSnapshot struct {
	GameID        string         `json:"game_id"`
	RoundID       string         `json:"round_id"`
	RoundNumber   int            `json:"round_number"`
	DrawerID      string         `json:"drawer_id"`
	WordMask      string         `json:"word_mask"`
	Players       []string       `json:"players"`
	Scores        map[string]int `json:"scores"`
	IsRoundActive bool           `json:"is_round_active"`
}

var (
	runtimeMutex  sync.RWMutex
	runtimeByRoom = make(map[string]*RuntimeState)
)

func SetRuntimeState(roomCode string, state *RuntimeState) {
	runtimeMutex.Lock()
	defer runtimeMutex.Unlock()
	runtimeByRoom[roomCode] = state
}

func DeleteRuntimeState(roomCode string) {
	runtimeMutex.Lock()
	defer runtimeMutex.Unlock()
	delete(runtimeByRoom, roomCode)
}

func WithRuntimeState(roomCode string, fn func(state *RuntimeState)) bool {
	runtimeMutex.Lock()
	defer runtimeMutex.Unlock()

	state, ok := runtimeByRoom[roomCode]
	if !ok {
		return false
	}

	fn(state)
	return true
}

func SnapshotRuntimeState(roomCode string) (RuntimeSnapshot, bool) {
	runtimeMutex.RLock()
	defer runtimeMutex.RUnlock()

	state, ok := runtimeByRoom[roomCode]
	if !ok {
		return RuntimeSnapshot{}, false
	}

	scoresCopy := make(map[string]int, len(state.Scores))
	for userID, score := range state.Scores {
		scoresCopy[userID] = score
	}

	return RuntimeSnapshot{
		GameID:        state.GameID,
		RoundID:       state.RoundID,
		RoundNumber:   state.RoundNumber,
		DrawerID:      state.DrawerID,
		WordMask:      maskWord(state.Word),
		Players:       append([]string(nil), state.PlayerOrder...),
		Scores:        scoresCopy,
		IsRoundActive: state.IsRoundActive,
	}, true
}

func maskWord(word string) string {
	if word == "" {
		return ""
	}

	runes := []rune(word)
	for i := range runes {
		if runes[i] != ' ' {
			runes[i] = '_'
		}
	}
	return string(runes)
}
