package memory

import (
	"errors"
	"sync"

	"github.com/adwski/webrtc-playground/backend/model"
)

const (
	defaultMaxParticipants = 2
)

var (
	ErrRoomIsFull   = errors.New("room is full")
	ErrRoomNotFound = errors.New("room is not found")
)

type MemStore struct {
	mx *sync.Mutex
	db map[string]*model.Room
}

func NewMemStore() *MemStore {
	return &MemStore{
		mx: &sync.Mutex{},
		db: make(map[string]*model.Room),
	}
}

func (ms *MemStore) CreateOrJoinRoom(roomID string, userID string) (*model.Room, error) {
	ms.mx.Lock()
	defer ms.mx.Unlock()

	room, ok := ms.db[roomID]
	if !ok {
		room = &model.Room{
			ID: roomID,
			Participants: map[string]model.Participant{
				userID: {ID: userID},
			},
		}
		ms.db[roomID] = room
		return room, nil
	}

	if len(room.Participants) == defaultMaxParticipants {
		if _, ok := room.Participants[userID]; !ok {
			return nil, ErrRoomIsFull
		}
	}

	room.Participants[userID] = model.Participant{
		ID: userID,
	}
	return room, nil
}

func (ms *MemStore) GetRoom(roomID string) (*model.Room, error) {
	ms.mx.Lock()
	defer ms.mx.Unlock()

	room, ok := ms.db[roomID]
	if !ok {
		return nil, ErrRoomNotFound
	}
	return room, nil
}
