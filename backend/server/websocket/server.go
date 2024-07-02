package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/adwski/webrtc-playground/backend/model"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

const (
	defaultShutdownDeadline = 10 * time.Second

	defaultSignalingSessionCloseTimeout = 2 * time.Second

	defaultWebsocketReadBufferSize     = 10000
	defaultWebsocketWriteBufferSize    = 10000
	defaultWebSocketMaxMessageSize     = 9000
	defaultWebSocketHandshakeTimeout   = 3 * time.Second
	defaultWebSocketCloseWriteDeadline = 2 * time.Second
	defaultWebSocketWriteDeadline      = 5 * time.Second

	// defaultPongWait - defaultPingInterval == is how long we give client to respond
	defaultPingInterval = 5 * time.Second
	defaultPongWait     = 7 * time.Second
)

var (
	ErrUnexpected = errors.New("unexpected server error")
)

type (
	SignalingService interface {
		CreateSignalingSession(context.Context, string, string, model.Wire) error
		DeleteSignalingSession(context.Context, string, string) error
	}

	Config struct {
		Logger           *zerolog.Logger
		SignalingService SignalingService
		ListenAddr       string
	}

	Server struct {
		svc SignalingService
		ws  *websocket.Upgrader
		*http.Server

		logger zerolog.Logger
	}
)

func NewServer(cfg Config) *Server {
	srv := &Server{
		logger: cfg.Logger.With().Str("component", "websocket-server").Logger(),
		svc:    cfg.SignalingService,
		ws: &websocket.Upgrader{
			HandshakeTimeout: defaultWebSocketHandshakeTimeout,
			ReadBufferSize:   defaultWebsocketReadBufferSize,
			WriteBufferSize:  defaultWebsocketWriteBufferSize,
			CheckOrigin:      func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/signal/room/{roomID}/user/{userID}", srv.signal)

	srv.Server = &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}
	return srv
}

func (srv *Server) Run(ctx context.Context, wg *sync.WaitGroup, errc chan<- error) {
	defer func() {
		srv.logger.Debug().Msg("server stopped")
		wg.Done()
	}()

	errSrv := make(chan error)
	go func() {
		errSrv <- srv.ListenAndServe()
	}()

	srv.logger.Info().Str("addr", srv.Addr).Msg("server started")

	select {
	case err := <-errSrv:
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

func (srv *Server) signal(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomID")
	userID := r.PathValue("userID")
	if roomID == "" || userID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	conn, err := srv.ws.Upgrade(w, r, nil)
	if err != nil {
		srv.logger.Error().Err(err).Msg("websocket upgrade failed")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	wire := model.NewWire()

	ctx, cancel := context.WithCancel(context.TODO()) // long-living wire context

	err = srv.svc.CreateSignalingSession(ctx, roomID, userID, wire)
	if err != nil {
		srv.logger.Error().Err(err).Msg("failed to create signaling session")
		w.WriteHeader(http.StatusInternalServerError)
		cancel()
		webSocketCloser(conn, &srv.logger)
		return
	}
	srv.logger.Debug().
		Str("roomID", roomID).
		Str("userID", userID).
		Msg("signaling session created")

	go srv.handleWSConn(ctx, cancel, conn, roomID, userID, wire)
}

func (srv *Server) destroySession(roomID, userID string, logger *zerolog.Logger) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(defaultSignalingSessionCloseTimeout))
	defer cancel()
	err := srv.svc.DeleteSignalingSession(ctx, roomID, userID)
	if err != nil {
		srv.logger.Error().Err(err).Msg("failed to delete signaling session")
		return
	}
	logger.Debug().
		Str("roomID", roomID).
		Str("userID", userID).
		Msg("signaling session ended")
}

func (srv *Server) handleWSConn(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *websocket.Conn,
	roomID string,
	userID string,
	wire model.Wire,
) {
	wg := &sync.WaitGroup{}

	logger := srv.logger.With().
		Str("roomID", roomID).
		Str("userID", userID).
		Logger()

	wg.Add(2)
	go func() {
		webSocketReceiver(ctx, wg, conn, userID, wire.RX, &logger)
		cancel()
	}()
	go func() {
		webSocketSender(ctx, wg, conn, wire.TX, &logger)
		cancel()
	}()

	wg.Wait()
	webSocketCloser(conn, &logger)
	srv.destroySession(roomID, userID, &logger)
}

func webSocketSender(
	ctx context.Context,
	wg *sync.WaitGroup,
	conn *websocket.Conn,
	tx <-chan model.Announcement,
	logger *zerolog.Logger,
) {
	pingTicker := time.NewTicker(defaultPingInterval)
	defer func() {
		pingTicker.Stop()
		wg.Done()
	}()
SendLoop:
	for {
		select {
		case <-ctx.Done():
			break SendLoop
		case <-pingTicker.C:
			wsErr := conn.SetWriteDeadline(time.Now().Add(defaultWebSocketWriteDeadline))
			if wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to set websocket write deadline")
				break SendLoop
			}
			wsErr = conn.WriteMessage(websocket.PingMessage, []byte{})
			if wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to send ping")
			}
			logger.Trace().Msg("ping sent")

		case msg, ok := <-tx:
			if !ok {
				break SendLoop
			}

			b, wsErr := json.Marshal(&msg)
			if wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to marshall outgoing message")
				break SendLoop
			}

			wsErr = conn.SetWriteDeadline(time.Now().Add(defaultWebSocketWriteDeadline))
			if wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to set websocket write deadline")
				break SendLoop
			}
			wsW, wsErr := conn.NextWriter(websocket.TextMessage)
			if wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to get websocket text writer")
				break SendLoop
			}
			_, wsErr = wsW.Write(b)
			if wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to write outgoing message")
				break SendLoop
			}
			wsErr = wsW.Close()
			if wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to close websocket writer")
				break SendLoop
			}
		}
	}
}

func webSocketReceiver(
	ctx context.Context,
	wg *sync.WaitGroup,
	conn *websocket.Conn,
	userID string,
	rx chan<- model.Announcement,
	logger *zerolog.Logger,
) {
	defer wg.Done()

	conn.SetReadLimit(defaultWebSocketMaxMessageSize)
	readDeadLineFunc := func(deadline time.Duration) error {
		return conn.SetReadDeadline(time.Now().Add(deadline))
	}
	conn.SetPongHandler(func(string) error {
		logger.Trace().Msg("got pong")
		return readDeadLineFunc(defaultPongWait)
	})
	err := readDeadLineFunc(defaultPongWait)
	if err != nil {
		logger.Error().Err(err).Msg("failed to set websocket read deadline")
		return
	}

RecvLoop:
	for {
		select {
		case <-ctx.Done():
			break RecvLoop
		default:
			_, msg, wsErr := conn.ReadMessage()
			if wsErr != nil {
				if websocket.IsCloseError(wsErr,
					websocket.CloseNormalClosure,
					websocket.CloseGoingAway) {
					logger.Warn().Err(wsErr).Msg("connection closed")
				} else {
					logger.Error().Err(wsErr).Msg("unexpected error during receive")
				}
				break RecvLoop
			}

			var ann model.Announcement
			if wsErr = json.Unmarshal(msg, &ann); wsErr != nil {
				logger.Error().Err(wsErr).Msg("failed to unmarshall incoming message")
			} else {
				ann.SRC = userID
				select {
				case rx <- ann:
				case <-ctx.Done():
					break RecvLoop
				}
			}
		}
	}
}

func webSocketCloser(conn *websocket.Conn, logger *zerolog.Logger) {
	wsErr := conn.SetWriteDeadline(time.Now().Add(defaultWebSocketCloseWriteDeadline))
	if wsErr != nil {
		logger.Error().Err(wsErr).Msg("failed to set websocket write deadline during closing")
	} else {
		wsErr = conn.WriteMessage(websocket.CloseMessage, []byte{})
		if wsErr != nil {
			logger.Error().Err(wsErr).Msg("failed to close websocket connection")
		}
	}
	wsErr = conn.Close()
	if wsErr != nil {
		logger.Error().Err(wsErr).Msg("failed to close websocket connection")
	}
}
