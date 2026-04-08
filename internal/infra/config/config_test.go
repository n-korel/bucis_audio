package config

import (
	"os"
	"testing"
)

func resetDotEnv(t *testing.T) {
	t.Helper()
	resetDotEnvForTest()
	t.Cleanup(resetDotEnvForTest)
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestParseBrs_Defaults(t *testing.T) {
	resetDotEnv(t)
	t.Setenv("CONTROL_ADDR", "")
	t.Setenv("CONTROL_PORT", "")
	t.Setenv("MEDIA_PORT", "")
	t.Setenv("METRICS_ADDR", "")
	t.Setenv("METRICS_LISTEN_PORT", "")
	t.Setenv("METRICS_SEND_PORT", "")
	t.Setenv("METRICS_REPLY_PORT", "")

	cfg, err := ParseBrs()
	if err != nil {
		t.Fatalf("ParseBrs: %v", err)
	}
	if cfg.ControlAddr != "192.168.1.255" {
		t.Fatalf("ControlAddr=%q", cfg.ControlAddr)
	}
	if cfg.ControlPort != 8889 || cfg.MediaPort != 5006 {
		t.Fatalf("ports: control=%d media=%d", cfg.ControlPort, cfg.MediaPort)
	}
	if cfg.MetricsAddr != cfg.ControlAddr {
		t.Fatalf("MetricsAddr=%q want %q", cfg.MetricsAddr, cfg.ControlAddr)
	}
	if cfg.MetricsListenPort != 8892 || cfg.MetricsSendPort != 0 || cfg.MetricsReplyPort != 8881 {
		t.Fatalf("metrics ports: listen=%d send=%d reply=%d", cfg.MetricsListenPort, cfg.MetricsSendPort, cfg.MetricsReplyPort)
	}
}

func TestParseBrs_CustomIntInvalid(t *testing.T) {
	resetDotEnv(t)
	t.Setenv("CONTROL_PORT", "x")

	_, err := ParseBrs()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseBucis_OK(t *testing.T) {
	resetDotEnv(t)
	t.Setenv("CONTROL_OFFSET_MS", "100")
	t.Setenv("CONTROL_ADDR", "127.0.0.1")
	t.Setenv("CONTROL_PORT", "1")
	t.Setenv("MEDIA_PORT", "2")
	t.Setenv("CONTROL_SEND_INTERVAL_MS", "50")
	t.Setenv("AUDIO_FILE", "/tmp/x.mp3")

	cfg, err := ParseBucis()
	if err != nil {
		t.Fatalf("ParseBucis: %v", err)
	}
	if cfg.ControlAddr != "127.0.0.1" || cfg.ControlPort != 1 || cfg.MediaPort != 2 {
		t.Fatalf("got %#v", cfg)
	}
	if cfg.OffsetMs != 100 || cfg.SendIntervalMs != 50 || cfg.AudioFile != "/tmp/x.mp3" {
		t.Fatalf("got %#v", cfg)
	}
}

func TestParseBucis_OffsetFallbackKey(t *testing.T) {
	resetDotEnv(t)
	t.Setenv("CONTROL_OFFSET_MS", "")
	t.Setenv("OFFSET_MS", "-50")
	t.Setenv("CONTROL_ADDR", "127.0.0.1")
	t.Setenv("CONTROL_PORT", "1")
	t.Setenv("MEDIA_PORT", "2")
	t.Setenv("SEND_INTERVAL_MS", "10")
	t.Setenv("AUDIO_FILE", "a.mp3")

	cfg, err := ParseBucis()
	if err != nil {
		t.Fatalf("ParseBucis: %v", err)
	}
	if cfg.OffsetMs != -50 || cfg.SendIntervalMs != 10 {
		t.Fatalf("got offset=%d interval=%d", cfg.OffsetMs, cfg.SendIntervalMs)
	}
}

func TestParseBucis_RequiredMissing(t *testing.T) {
	resetDotEnv(t)
	t.Setenv("CONTROL_ADDR", "")
	t.Setenv("CONTROL_PORT", "")
	t.Setenv("MEDIA_PORT", "")
	t.Setenv("CONTROL_OFFSET_MS", "0")
	t.Setenv("OFFSET_MS", "")
	t.Setenv("CONTROL_SEND_INTERVAL_MS", "1")
	t.Setenv("SEND_INTERVAL_MS", "")
	t.Setenv("AUDIO_FILE", "")

	_, err := ParseBucis()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseBucis_InvalidPort(t *testing.T) {
	resetDotEnv(t)
	t.Setenv("CONTROL_OFFSET_MS", "0")
	t.Setenv("CONTROL_ADDR", "127.0.0.1")
	t.Setenv("CONTROL_PORT", "nope")
	t.Setenv("MEDIA_PORT", "2")
	t.Setenv("CONTROL_SEND_INTERVAL_MS", "1")
	t.Setenv("AUDIO_FILE", "a.mp3")

	_, err := ParseBucis()
	if err == nil {
		t.Fatal("expected error")
	}
}
