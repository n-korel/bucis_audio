package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Bucis struct {
	ControlAddr    string
	ControlPort    int
	MediaPort      int
	OffsetMs       int64
	SendIntervalMs int
	AudioFile      string
}

type Brs struct {
	ControlAddr string
	ControlPort int
	MediaPort   int

	MetricsAddr       string
	MetricsListenPort int
	MetricsSendPort   int
	MetricsReplyPort  int
}

func ParseBucis() (Bucis, error) {
	if err := loadDotEnv(); err != nil {
		return Bucis{}, err
	}

	offsetMs, err := getEnvFallbackRequired(func(s string) (int64, error) {
		return strconv.ParseInt(s, 10, 64)
	}, "int64", "CONTROL_OFFSET_MS", "OFFSET_MS")
	if err != nil {
		return Bucis{}, err
	}

	controlAddr, err := getEnvStringRequired("CONTROL_ADDR")
	if err != nil {
		return Bucis{}, err
	}

	controlPort, err := getEnvIntRequired("CONTROL_PORT")
	if err != nil {
		return Bucis{}, err
	}

	mediaPort, err := getEnvIntRequired("MEDIA_PORT")
	if err != nil {
		return Bucis{}, err
	}

	sendIntervalMs, err := getEnvFallbackRequired(strconv.Atoi, "integer", "CONTROL_SEND_INTERVAL_MS", "SEND_INTERVAL_MS")
	if err != nil {
		return Bucis{}, err
	}

	audioFile, err := getEnvStringRequired("AUDIO_FILE")
	if err != nil {
		return Bucis{}, err
	}

	return Bucis{
		ControlAddr:    controlAddr,
		ControlPort:    controlPort,
		MediaPort:      mediaPort,
		OffsetMs:       offsetMs,
		SendIntervalMs: sendIntervalMs,
		AudioFile:      audioFile,
	}, nil
}

func ParseBrs() (Brs, error) {
	if err := loadDotEnv(); err != nil {
		return Brs{}, err
	}

	controlAddr := getEnvStringDefault("CONTROL_ADDR", "192.168.1.255")

	controlPort, err := getEnvIntDefault("CONTROL_PORT", 8889)
	if err != nil {
		return Brs{}, err
	}

	mediaPort, err := getEnvIntDefault("MEDIA_PORT", 5006)
	if err != nil {
		return Brs{}, err
	}

	metricsAddr := getEnvStringDefault("METRICS_ADDR", controlAddr)

	metricsListenPort, err := getEnvIntDefault("METRICS_LISTEN_PORT", 8892)
	if err != nil {
		return Brs{}, err
	}

	metricsSendPort, err := getEnvIntDefault("METRICS_SEND_PORT", 8892)
	if err != nil {
		return Brs{}, err
	}

	metricsReplyPort, err := getEnvIntDefault("METRICS_REPLY_PORT", 8881)
	if err != nil {
		return Brs{}, err
	}

	return Brs{
		ControlAddr: controlAddr,
		ControlPort: controlPort,
		MediaPort:   mediaPort,

		MetricsAddr:       metricsAddr,
		MetricsListenPort: metricsListenPort,
		MetricsSendPort:   metricsSendPort,
		MetricsReplyPort:  metricsReplyPort,
	}, nil
}

func getEnvStringDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvIntDefault(key string, def int) (int, error) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
		}
		return n, nil
	}
	return def, nil
}

func getEnvStringRequired(key string) (string, error) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is required (set it in .env or environment)", key)
}

func getEnvIntRequired(key string) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return 0, fmt.Errorf("%s is required (set it in .env or environment)", key)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
	}
	return n, nil
}

func getEnvFallbackRequired[T int | int64](parse func(string) (T, error), typeDesc string, keys ...string) (T, error) {
	var zero T
	for _, key := range keys {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			n, err := parse(v)
			if err == nil {
				return n, nil
			}
			return zero, fmt.Errorf("%s must be a valid %s: %w", key, typeDesc, err)
		}
	}
	switch len(keys) {
	case 0:
		return zero, fmt.Errorf("no environment variable keys provided")
	case 1:
		return zero, fmt.Errorf("%s is required (set it in .env or environment)", keys[0])
	case 2:
		return zero, fmt.Errorf("%s (or %s) is required (set it in .env or environment)", keys[0], keys[1])
	default:
		return zero, fmt.Errorf("one of %s is required (set it in .env or environment)", strings.Join(keys, ", "))
	}
}
