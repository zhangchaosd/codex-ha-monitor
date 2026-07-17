package identity

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadOrCreate() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configDir, "cma")
	path := filepath.Join(dir, "installation_id")
	if data, readErr := os.ReadFile(path); readErr == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value, nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	value, err := randomUUID()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
		return "", err
	}
	return value, nil
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
