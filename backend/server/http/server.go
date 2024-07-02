package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/adwski/webrtc-playground/backend/model"
	"github.com/rs/zerolog"
)

const (
	defaultShutdownDeadline = 10 * time.Second
)

var (
	ErrUnexpected = errors.New("unexpected server error")
)

type RoomService interface {
	JoinRoom(roomID string, userID string) (*model.Room, error)
}

type JoinRequest struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

type GenericResponse struct {
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type Server struct {
	logger zerolog.Logger
	svc    RoomService
	*http.Server
}

type Config struct {
	Logger      *zerolog.Logger
	RoomService RoomService
	ListenAddr  string
}

func NewServer(cfg Config) *Server {
	srv := &Server{
		logger: cfg.Logger.With().Str("component", "api-server").Logger(),
		svc:    cfg.RoomService,
	}

	r := http.NewServeMux()
	r.HandleFunc("POST /api/room", srv.joinRoom)
	r.HandleFunc("OPTIONS /", corsHandler)

	srv.Server = &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: r,
	}
	return srv
}

func corsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.WriteHeader(http.StatusNoContent)
}

func (srv *Server) joinRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	var (
		body    []byte
		joinReq JoinRequest
	)
	body, _ = io.ReadAll(r.Body)
	defer func() {
		_ = r.Body.Close()
	}()
	if err := json.Unmarshal(body, &joinReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	srv.logger.Trace().Any("request", joinReq).Msg("got join request")

	_, err := srv.svc.JoinRoom(joinReq.RoomID, joinReq.UserID)
	if err != nil {
		b, errJ := json.Marshal(&GenericResponse{Error: err.Error()})
		if errJ != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeBytes(w, http.StatusConflict, b)
		return
	}

	b, err := json.Marshal(&GenericResponse{Message: "OK"})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeBytes(w, http.StatusOK, b)
}

func writeBytes(w http.ResponseWriter, code int, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(code)
	if _, err := w.Write(b); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

func (srv *Server) Run(ctx context.Context, wg *sync.WaitGroup, errc chan<- error) {
	defer func() {
		srv.logger.Debug().Msg("server stopped")
		wg.Done()
	}()

	hErr := make(chan error)
	go func() {
		hErr <- srv.ListenAndServe()
	}()

	srv.logger.Info().Str("addr", srv.Addr).Msg("server started")

	select {
	case err := <-hErr:
		if !errors.Is(err, http.ErrServerClosed) {
			errc <- errors.Join(ErrUnexpected, err)
		}
	case <-ctx.Done():
		shCtx, shCancel := context.WithTimeout(context.Background(), defaultShutdownDeadline)
		defer shCancel()
		if err := srv.Shutdown(shCtx); err != nil {
			srv.logger.Error().Err(err).Msg("server shutdown failed")
		}
	}
}
