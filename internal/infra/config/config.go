package config

import (
	"fmt"
	"os"
	"strconv"
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
}

func ParseBucis() (Bucis, error) {
	if err := loadDotEnv(); err != nil {
		return Bucis{}, err
	}

	offsetMs, err := getEnvInt64FallbackRequired("CONTROL_OFFSET_MS", "OFFSET_MS")
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

	sendIntervalMs, err := getEnvIntFallbackRequired("CONTROL_SEND_INTERVAL_MS", "SEND_INTERVAL_MS")
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

	return Brs{
		ControlAddr: controlAddr,
		ControlPort: controlPort,
		MediaPort:   mediaPort,
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

func getEnvInt64FallbackRequired(primaryKey, secondaryKey string) (int64, error) {
	if v, ok := os.LookupEnv(primaryKey); ok && v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return n, nil
		}
		return 0, fmt.Errorf("%s must be a valid int64: %w", primaryKey, err)
	}
	if v, ok := os.LookupEnv(secondaryKey); ok && v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return n, nil
		}
		return 0, fmt.Errorf("%s must be a valid int64: %w", secondaryKey, err)
	}
	return 0, fmt.Errorf("%s (or %s) is required (set it in .env or environment)", primaryKey, secondaryKey)
}

func getEnvIntFallbackRequired(primaryKey, secondaryKey string) (int, error) {
	if v, ok := os.LookupEnv(primaryKey); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n, nil
		}
		return 0, fmt.Errorf("%s must be a valid integer: %w", primaryKey, err)
	}
	if v, ok := os.LookupEnv(secondaryKey); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n, nil
		}
		return 0, fmt.Errorf("%s must be a valid integer: %w", secondaryKey, err)
	}
	return 0, fmt.Errorf("%s (or %s) is required (set it in .env or environment)", primaryKey, secondaryKey)
}
