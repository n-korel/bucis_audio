package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"announcer_simulator/internal/control/protocol"
	"announcer_simulator/internal/control/udp"
	"announcer_simulator/internal/infra/config"
	"announcer_simulator/internal/media/mp3"
	"announcer_simulator/internal/media/sender"
	"announcer_simulator/pkg/log"
)

func main() {
	var controlAddrVal string
	var controlPortVal int
	var mediaPortVal int
	var offsetMsVal int64
	var audioFileVal string

	flag.StringVar(&controlAddrVal, "control-addr", "", "UDP broadcast address (default from CONTROL_ADDR / auto-detect)")
	flag.IntVar(&controlPortVal, "control-port", 0, "UDP port (default from CONTROL_PORT)")
	flag.IntVar(&mediaPortVal, "media-port", 0, "RTP media port (default from MEDIA_PORT)")
	flag.Int64Var(&offsetMsVal, "offset-ms", 0, "offset for t0 in milliseconds (default from CONTROL_OFFSET_MS / OFFSET_MS)")
	flag.StringVar(&audioFileVal, "audio-file", "", "path to mp3 audio file (default from AUDIO_FILE)")
	flag.Parse()
	log.Init(os.Getenv("LOG_FORMAT"))
	logger := log.With("role", "bucis")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.ParseBucis()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "control-addr":
			cfg.ControlAddr = controlAddrVal
		case "control-port":
			cfg.ControlPort = controlPortVal
		case "media-port":
			cfg.MediaPort = mediaPortVal
		case "offset-ms":
			cfg.OffsetMs = offsetMsVal
		case "audio-file":
			cfg.AudioFile = audioFileVal
		}
	})
	if cfg.OffsetMs <= 0 {
		fmt.Fprintln(os.Stderr, "CONTROL_OFFSET_MS/--offset-ms must be > 0")
		os.Exit(1)
	}

	controlSender, err := udp.NewSender(cfg.ControlAddr, cfg.ControlPort)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var closeControlOnce sync.Once
	closeControl := func() {
		closeControlOnce.Do(func() {
			if err := controlSender.Close(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		})
	}
	defer closeControl()

	if cfg.SendIntervalMs <= 0 {
		fmt.Fprintln(os.Stderr, "CONTROL_SEND_INTERVAL_MS must be > 0")
		os.Exit(1)
	}

	pcm, err := mp3.LoadPCM8kMono(cfg.AudioFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to decode audio file %q: %v\n", cfg.AudioFile, err)
		os.Exit(1)
	}

	mediaSender, err := sender.New(cfg.ControlAddr, cfg.MediaPort)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var closeMediaOnce sync.Once
	closeMedia := func() {
		closeMediaOnce.Do(func() {
			if err := mediaSender.Close(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		})
	}
	defer closeMedia()

	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received, stopping")
		_, _ = controlSender.Send([]byte("sound_stop"))
		closeMedia()
		closeControl()
	}()

	sendTicker := time.NewTicker(time.Duration(cfg.SendIntervalMs) * time.Millisecond)
	defer sendTicker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		tSend := time.Now().UnixMilli()
		t0 := tSend + cfg.OffsetMs
		sessionID := fmt.Sprintf("%08x", rand.Uint32())
		msg := protocol.FormatSoundStart(t0, sessionID)
		if _, err := controlSender.Send([]byte(msg)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logger.Info("sound_start sent", "session_id", sessionID)

		if err := mediaSender.StreamAt(ctx, t0, pcm); err != nil && err != context.Canceled {
			fmt.Fprintf(os.Stderr, "media stream error: %v\n", err)
			os.Exit(1)
		}
		if _, err := controlSender.Send([]byte("sound_stop")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logger.Info("sound_stop sent")

		select {
		case <-ctx.Done():
			return
		case <-sendTicker.C:
		}
	}
}
