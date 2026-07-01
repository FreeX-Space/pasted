package main

import (
	"net"
	"testing"
)

func TestParseArgsListenDefault(t *testing.T) {
	cfg, err := parseArgs([]string{"-L"})
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	if cfg.mode != "listen" {
		t.Fatalf("mode = %q, want listen", cfg.mode)
	}
	if cfg.endpoint != "tls://0.0.0.0:48217" {
		t.Fatalf("endpoint = %q", cfg.endpoint)
	}
}

func TestParseArgsForwardRequiresEndpoint(t *testing.T) {
	if _, err := parseArgs([]string{"-F"}); err == nil {
		t.Fatal("expected -F without endpoint to fail")
	}
}

func TestParseTLSEndpointDefaultsPort(t *testing.T) {
	addr, err := parseTLSEndpoint("tls://1.1.1.1", "")
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	if addr != "1.1.1.1:48217" {
		t.Fatalf("addr = %q", addr)
	}
}

func TestParseTLSEndpointRejectsUnsupportedScheme(t *testing.T) {
	if _, err := parseTLSEndpoint("tcp://1.1.1.1:48217", ""); err == nil {
		t.Fatal("expected unsupported scheme to fail")
	}
}

func TestIsUsableLocalIPv4RejectsLinkLocal(t *testing.T) {
	if isUsableLocalIPv4(net.ParseIP("169.254.34.29").To4()) {
		t.Fatal("169.254.x.x should not be selected as local identity")
	}
	if !isUsableLocalIPv4(net.ParseIP("192.168.31.23").To4()) {
		t.Fatal("private LAN IPv4 should be usable")
	}
}
