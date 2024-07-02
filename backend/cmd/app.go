package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	httpServer "github.com/adwski/webrtc-playground/backend/server/http"
	websocketServer "github.com/adwski/webrtc-playground/backend/server/websocket"
	"github.com/adwski/webrtc-playground/backend/service"
	store "github.com/adwski/webrtc-playground/backend/storage/memory"
	sw "github.com/adwski/webrtc-playground/backend/switch"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	fs := pflag.NewFlagSet("main", pflag.ContinueOnError)

	var (
		apiListenAddr = fs.StringP("api-listen-addr", "a", ":8080", "api listen address")
		wsListenAddr  = fs.StringP("ws-listen-addr", "w", ":8888", "websocket signaling listen address")
		logLevel      = fs.StringP("log-level", "l", "debug", "log level")
	)
	if err := fs.Parse(os.Args[1:]); err != nil {
		logger.Fatal().Err(err).Msg("failed to parse command line arguments")
	}

	lvl, err := zerolog.ParseLevel(*logLevel)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to parse loglevel")
	}
	logger = logger.Level(lvl)

	svc := service.NewService(service.Config{
		RoomStore: store.NewMemStore(),
		Switch:    sw.NewSwitch(&logger),
		Logger:    &logger,
	})
	httpSrv := httpServer.NewServer(httpServer.Config{
		Logger:      &logger,
		RoomService: svc,
		ListenAddr:  *apiListenAddr,
	})
	wsSrv := websocketServer.NewServer(websocketServer.Config{
		Logger:           &logger,
		SignalingService: svc,
		ListenAddr:       *wsListenAddr,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var (
		wg   = &sync.WaitGroup{}
		errc = make(chan error, 2)
	)
	wg.Add(2)
	go httpSrv.Run(ctx, wg, errc)
	go wsSrv.Run(ctx, wg, errc)

	select {
	case err = <-errc:
		logger.Error().Err(err).Msg("unexpected server error, shutting down")
	case <-ctx.Done():
		logger.Warn().Msg("interrupted")
	}
	cancel()
	wg.Wait()
}
