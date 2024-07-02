package service

import (
	"context"
	"errors"

	"github.com/adwski/webrtc-playground/backend/model"
	"github.com/rs/zerolog"
)

var (
	ErrJoin       = errors.New("unable to join room")
	ErrGet        = errors.New("unable to get room")
	ErrNotAMember = errors.New("user is not a member of this room")
	ErrConnect    = errors.New("unable to connect")
	ErrDisconnect = errors.New("unable to disconnect")
)

type (
	RoomStore interface {
		CreateOrJoinRoom(roomID string, userID string) (*model.Room, error)
		GetRoom(roomID string) (*model.Room, error)
	}

	Switch interface {
		Connect(ctx context.Context, roomID string, userID string, wire model.Wire) error
		Disconnect(roomID string, userID string) error
		Broadcast(ctx context.Context, ann model.Announcement, roomID string) error
	}

	Service struct {
		store  RoomStore
		sw     Switch
		logger zerolog.Logger
	}

	Config struct {
		RoomStore RoomStore
		Switch    Switch
		Logger    *zerolog.Logger
	}
)

func NewService(cfg Config) *Service {
	return &Service{
		store:  cfg.RoomStore,
		sw:     cfg.Switch,
		logger: cfg.Logger.With().Str("component", "api").Logger(),
	}
}

func (svc *Service) CreateSignalingSession(ctx context.Context, roomID, userID string, wire model.Wire) error {
	room, err := svc.store.GetRoom(roomID)
	if err != nil {
		return errors.Join(ErrGet, err)
	}
	if _, ok := room.Participants[userID]; !ok {
		return ErrNotAMember
	}
	err = svc.sw.Connect(ctx, roomID, userID, wire)
	if err != nil {
		return errors.Join(ErrConnect, err)
	}
	svc.logger.Debug().
		Str("userID", userID).
		Str("roomID", roomID).
		Msg("signaling session connected")

	go func() {
		ann := model.Announcement{
			Type: model.AnnouncementTypeJoined,
			SRC:  userID,
		}
		_ = svc.sw.Broadcast(ctx, ann, roomID)
	}()
	return nil
}

func (svc *Service) DeleteSignalingSession(ctx context.Context, roomID, userID string) error {
	err := svc.sw.Disconnect(roomID, userID)
	if err != nil {
		return errors.Join(ErrDisconnect, err)
	}
	svc.logger.Debug().
		Str("userID", userID).
		Str("roomID", roomID).
		Msg("signaling session deleted")

	go func() {
		ann := model.Announcement{
			SRC:  userID,
			Type: model.AnnouncementTypeLeft,
		}
		_ = svc.sw.Broadcast(ctx, ann, roomID)
	}()
	return nil
}

func (svc *Service) JoinRoom(roomID, userID string) (*model.Room, error) {
	room, err := svc.store.CreateOrJoinRoom(roomID, userID)
	if err != nil {
		return nil, errors.Join(ErrJoin, err)
	}
	svc.logger.Debug().
		Str("userID", userID).
		Str("roomID", roomID).
		Msg("user joined room")
	return room, nil
}
