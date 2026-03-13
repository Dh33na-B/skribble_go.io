package game

import (
	"sync"
)

var (
	Rooms = make(map[string]*Room)
	Mutex sync.Mutex
)
