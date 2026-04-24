package ssrf

import (
	"net"
	"testing"
)

func TestIsPrivateIP_Loopback(t *testing.T) {
	ip := net.ParseIP("127.0.0.1")
	if !IsPrivateIP(ip) {
		t.Errorf("expected 127.0.0.1 to be private")
	}
}

func TestIsPrivateIP_LinkLocal(t *testing.T) {
	ip := net.ParseIP("169.254.42.1")
	if !IsPrivateIP(ip) {
		t.Errorf("expected 169.254.42.1 to be private (link-local / cloud metadata)")
	}
}

func TestIsPrivateIP_RFC1918_10(t *testing.T) {
	ip := net.ParseIP("10.0.0.5")
	if !IsPrivateIP(ip) {
		t.Errorf("expected 10.0.0.5 to be private")
	}
}

func TestIsPrivateIP_IPv6Loopback(t *testing.T) {
	ip := net.ParseIP("::1")
	if !IsPrivateIP(ip) {
		t.Errorf("expected ::1 to be private")
	}
}

func TestIsPrivateIP_PublicIP(t *testing.T) {
	ip := net.ParseIP("8.8.8.8")
	if IsPrivateIP(ip) {
		t.Errorf("expected 8.8.8.8 to be public")
	}
}
