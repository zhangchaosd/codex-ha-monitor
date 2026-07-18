package discovery

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/grandcat/zeroconf"
)

const ServiceType = "_codex-monitor._tcp"

// Advertise publishes one Codex Monitor Agent through mDNS/Zeroconf. Failure
// is non-fatal because manual URL setup must continue to work on restricted
// networks and platforms without multicast support.
func Advertise(ctx context.Context, port int, installationID, version, schemaVersion string) error {
	hostname, _ := os.Hostname()
	instance := serviceInstance(hostname, installationID)
	server, err := zeroconf.Register(instance, ServiceType, "local.", port, []string{
		"installation_id=" + installationID,
		"version=" + version,
		"schema_version=" + schemaVersion,
		"auth_required=true",
		"path=/",
	}, nil)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		server.Shutdown()
	}()
	return nil
}

func serviceInstance(hostname, installationID string) string {
	shortID := installationID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	hostname = strings.Map(func(value rune) rune {
		if value == '.' || value == '/' || value == '\\' || unicode.IsControl(value) {
			return '-'
		}
		return value
	}, hostname)
	hostname = strings.Trim(hostname, " -")
	if hostname == "" {
		hostname = "host"
	}
	prefix := "Codex Monitor "
	suffix := fmt.Sprintf(" %s", shortID)
	maxHostBytes := 63 - len(prefix) - len(suffix)
	for len([]byte(hostname)) > maxHostBytes {
		_, size := utf8.DecodeLastRuneInString(hostname)
		hostname = hostname[:len(hostname)-size]
	}
	return prefix + hostname + suffix
}
