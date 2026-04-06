package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"net"
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
	var soundTypeVal int

	flag.StringVar(&controlAddrVal, "control-addr", "", "UDP broadcast address (default from CONTROL_ADDR / auto-detect)")
	flag.IntVar(&controlPortVal, "control-port", 0, "UDP port (default from CONTROL_PORT)")
	flag.IntVar(&mediaPortVal, "media-port", 0, "RTP media port (default from MEDIA_PORT)")
	flag.Int64Var(&offsetMsVal, "offset-ms", 0, "offset for t0 in milliseconds (default from CONTROL_OFFSET_MS / OFFSET_MS)")
	flag.StringVar(&audioFileVal, "audio-file", "", "path to mp3 audio file (default from AUDIO_FILE)")
	flag.IntVar(&soundTypeVal, "sound-type", 1, "sound_start type: 1=file, 2=mic (default 1)")
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
	if soundTypeVal != 1 && soundTypeVal != 2 {
		fmt.Fprintln(os.Stderr, "--sound-type must be 1 or 2")
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
	pcmDur := time.Duration(int64(len(pcm))*1000/8000) * time.Millisecond
	if pcmDur <= 0 {
		pcmDur = 1 * time.Second
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

shutdownLoop:
	for {
		if ctx.Err() != nil {
			break shutdownLoop
		}

		tSend := time.Now().UnixMilli()
		t0 := tSend + cfg.OffsetMs
		sessionID := fmt.Sprintf("%08x", rand.Uint32())
		msg := protocol.FormatSoundStart(soundTypeVal, t0, sessionID)
		if _, err := controlSender.Send([]byte(msg)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logger.Info("sound_start sent", "session_id", sessionID, "type", soundTypeVal)

		if soundTypeVal == 1 {
			if err := mediaSender.StreamAt(ctx, t0, pcm); err != nil &&
				!errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
				fmt.Fprintf(os.Stderr, "media stream error: %v\n", err)
				os.Exit(1)
			}
		} else {
			untilT0 := time.Until(time.UnixMilli(t0))
			if untilT0 < 0 {
				untilT0 = 0
			}
			wait := time.NewTimer(untilT0 + pcmDur)
			select {
			case <-ctx.Done():
				if !wait.Stop() {
					<-wait.C
				}
				break shutdownLoop
			case <-wait.C:
			}
		}
		if _, err := controlSender.Send([]byte("sound_stop")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logger.Info("sound_stop sent")

		wait := time.NewTimer(time.Duration(cfg.SendIntervalMs) * time.Millisecond)
		select {
		case <-ctx.Done():
			if !wait.Stop() {
				<-wait.C
			}
			break shutdownLoop
		case <-wait.C:
		}
	}

	logger.Info("shutdown: sending sound_stop")
	if _, err := controlSender.Send([]byte("sound_stop")); err != nil {
		logger.Warn("shutdown: sound_stop send failed", "err", err)
	}
}
