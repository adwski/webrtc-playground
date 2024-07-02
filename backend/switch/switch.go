package _switch

import (
	"context"
	"sync"
	"time"

	"github.com/adwski/webrtc-playground/backend/model"
	"github.com/rs/zerolog"
)

const (
	defaultFwdTimout = time.Second
)

type Switch struct {
	logger zerolog.Logger
	mx     *sync.RWMutex
	fwd    map[string]map[string]model.Wire
}

func NewSwitch(logger *zerolog.Logger) *Switch {
	return &Switch{
		logger: logger.With().Str("component", "switch").Logger(),
		mx:     &sync.RWMutex{},
		fwd:    make(map[string]map[string]model.Wire),
	}
}

func (sw *Switch) Disconnect(instance, endpoint string) error {
	sw.mx.Lock()
	defer func() {
		sw.mx.Unlock()
		sw.logger.Debug().
			Str("instance", instance).
			Str("endpoint", endpoint).
			Msg("endpoint disconnected")
	}()

	inst, ok := sw.fwd[instance]
	if ok {
		delete(inst, endpoint)
		sw.fwd[instance] = inst
	}
	return nil
}

func (sw *Switch) Connect(ctx context.Context, instance string, endpoint string, wire model.Wire) error {
	sw.mx.Lock()
	defer func() {
		sw.mx.Unlock()
		sw.logger.Debug().
			Str("instance", instance).
			Str("endpoint", endpoint).
			Msg("endpoint connected")
		go sw.forwardAnnouncements(ctx, instance, wire.RX)
	}()

	inst, ok := sw.fwd[instance]
	if !ok {
		inst = make(map[string]model.Wire)
	}
	inst[endpoint] = wire
	sw.fwd[instance] = inst
	return nil
}

func (sw *Switch) forwardAnnouncements(ctx context.Context, instance string, rx <-chan model.Announcement) {
fwdLoop:
	for {
		select {
		case <-ctx.Done():
			break fwdLoop
		case ann := <-rx:
			if ann.SRC == "" {
				sw.logger.Error().
					Str("instance", instance).
					Msg("announcement with empty src")
			} else {
				if !sw.forward(ctx, ann, instance) {
					sw.logger.Debug().
						Str("instance", instance).
						Str("src", ann.SRC).
						Msg("incoming announce was dropped, nowhere to forward")
				}
			}
		}
	}
}

func (sw *Switch) Broadcast(ctx context.Context, ann model.Announcement, instance string) error {
	ann.DST = "" // clear dst just in case
	if !sw.forward(ctx, ann, instance) {
		sw.logger.Debug().
			Str("instance", instance).
			Str("type", ann.Type).
			Str("src", ann.SRC).
			Msg("broadcast did not reach anyone")
	}
	return nil
}

func (sw *Switch) forward(ctx context.Context, ann model.Announcement, instance string) bool {
	var (
		sent   bool
		logger = sw.logger.With().
			Str("instance", instance).
			Str("type", ann.Type).
			Str("src", ann.SRC).Logger()
	)

	sw.mx.RLock()
	inst := sw.fwd[instance]
	sw.mx.RUnlock()

	if ann.DST == "" {
		// broadcast announce

		for dst, wire := range inst {
			if dst != ann.SRC {
				annSent, canceled := send(ctx, ann, wire.TX, &sw.logger)
				if canceled {
					break
				}
				if annSent {
					sent = true
				}
			}
		}

	} else {
		// send to a particular endpoint

		wire, ok := inst[ann.DST]
		if !ok {
			logger.Debug().Str("dst", ann.DST).Msg("cannot forward, dst not found")
		} else {
			sent, _ = send(ctx, ann, wire.TX, &logger)
		}
	}
	return sent
}

func send(ctx context.Context, ann model.Announcement, tx chan<- model.Announcement, logger *zerolog.Logger) (bool, bool) {
	var sent, canceled bool
	tCh := time.NewTimer(defaultFwdTimout)
	select {
	case <-ctx.Done():
		canceled = true
	case <-tCh.C:
		logger.Error().Str("dst", ann.DST).Msg("dead endpoint")
	case tx <- ann:
		logger.Debug().Str("dst", ann.DST).Msg("announce is forwarded")
		sent = true
	}
	tCh.Stop()
	return sent, canceled
}
