package discovery

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestServiceInstanceIsOneBoundedDNSLabel(t *testing.T) {
	instance := serviceInstance(
		"非常长的主机名称.含有域名.以及/无效字符/very-long-hostname",
		"12345678-1234-5678-9abc-def012345678",
	)
	if len([]byte(instance)) > 63 {
		t.Fatalf("instance is %d bytes: %q", len([]byte(instance)), instance)
	}
	if !utf8.ValidString(instance) {
		t.Fatalf("instance is invalid UTF-8: %q", instance)
	}
	if strings.ContainsAny(instance, ".\\/") {
		t.Fatalf("instance contains a DNS label separator: %q", instance)
	}
	if !strings.HasSuffix(instance, " 12345678") {
		t.Fatalf("instance lost the stable identity suffix: %q", instance)
	}
}
