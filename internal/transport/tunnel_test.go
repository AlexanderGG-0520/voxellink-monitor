package transport

import "testing"

func TestEmptyBinaryUsesCloudflared(t *testing.T) {
	tunnel := NewAccessTunnel("", 0)
	if tunnel.binary != "cloudflared" {
		t.Fatalf("binary = %q", tunnel.binary)
	}
}
