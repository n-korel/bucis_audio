package protocol

import (
	"encoding/hex"
	"strconv"
	"strings"
)

type SoundStart struct {
	T0        int64
	SessionID string
}

type SoundStop struct {
	Args string
}

func Parse(b []byte) (start *SoundStart, stop *SoundStop, ok bool) {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil, nil, false
	}

	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, nil, false
	}
	cmd := fields[0]
	rest := strings.TrimSpace(s[len(cmd):])

	switch cmd {
	case "sound_start":
		parts := make([]string, 0, 2)
		for _, part := range strings.Split(rest, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			parts = append(parts, part)
			if len(parts) > 2 {
				return nil, nil, false
			}
		}
		if len(parts) < 1 {
			return nil, nil, false
		}
		t0Str := parts[0]
		t0, err := strconv.ParseInt(t0Str, 10, 64)
		if err != nil {
			return nil, nil, false
		}

		sessionID := ""
		if len(parts) == 2 {
			sessionID = parts[1]
			if len(sessionID) != 8 {
				return nil, nil, false
			}
			if _, err := hex.DecodeString(sessionID); err != nil {
				return nil, nil, false
			}
		}
		return &SoundStart{T0: t0, SessionID: sessionID}, nil, true

	case "sound_stop":
		return nil, &SoundStop{Args: rest}, true

	default:
		return nil, nil, false
	}
}

func FormatSoundStart(t0 int64, sessionID string) string {
	msg := "sound_start " +
		strconv.FormatInt(t0, 10) + ";"
	if sessionID != "" {
		msg += sessionID + ";"
	}
	return msg
}
