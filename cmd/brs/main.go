package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"announcer_simulator/internal/brs"
	"announcer_simulator/internal/infra/config"
	"announcer_simulator/pkg/log"
)

func buildInfoString() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi == nil {
		return "unknown"
	}
	var rev, dirty string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value
		}
	}
	if rev == "" {
		return bi.GoVersion
	}
	if dirty == "true" {
		return rev + " (modified)"
	}
	return rev
}

func main() {
	var (
		controlAddr string
		controlPort int
		mediaPort   int

		metricsAddr       string
		metricsListenPort int
		metricsSendPort   int
		metricsReplyPort  int
	)
	var printVersion bool

	flag.StringVar(&controlAddr, "control-addr", "", "UDP broadcast address (default from CONTROL_ADDR / auto-detect)")
	flag.IntVar(&controlPort, "control-port", 0, "UDP port (default from CONTROL_PORT)")
	flag.IntVar(&mediaPort, "media-port", 0, "RTP media port (default from MEDIA_PORT)")
	flag.BoolVar(&printVersion, "version", false, "print build version and exit")

	flag.StringVar(&metricsAddr, "metrics-addr", "", "Metrics UDP address (default from METRICS_ADDR, fallback=control-addr)")
	flag.IntVar(&metricsListenPort, "metrics-listen-port", 0, "UDP port to receive get_metrics (default from METRICS_LISTEN_PORT)")
	flag.IntVar(&metricsSendPort, "metrics-send-port", 0, "Destination UDP port for sending metrics (default from METRICS_SEND_PORT)")
	flag.IntVar(&metricsReplyPort, "metrics-reply-port", 0, "UDP reply port for get_metrics (default from METRICS_REPLY_PORT)")
	flag.Parse()
	if printVersion {
		fmt.Println(buildInfoString())
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.ParseBrs()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "control-addr":
			cfg.ControlAddr = controlAddr
		case "control-port":
			cfg.ControlPort = controlPort
		case "media-port":
			cfg.MediaPort = mediaPort
		case "metrics-addr":
			cfg.MetricsAddr = metricsAddr
		case "metrics-listen-port":
			cfg.MetricsListenPort = metricsListenPort
		case "metrics-send-port":
			cfg.MetricsSendPort = metricsSendPort
		case "metrics-reply-port":
			cfg.MetricsReplyPort = metricsReplyPort
		}
	})

	brsName := os.Getenv("BRS_NAME")
	if brsName == "" {
		brsName = "brs"
	}

	log.Init(os.Getenv("LOG_FORMAT"))
	logger := log.With("node", brsName, "role", "brs")
	logger.Info("brs started", "build", buildInfoString())

	svc := brs.New(cfg, brsName, logger)
	if err := svc.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
