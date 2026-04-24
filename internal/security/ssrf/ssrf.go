// Package ssrf provides protection against Server-Side Request Forgery attacks.
package ssrf

import (
	"fmt"
	"net"
	"net/url"
)

// privateRanges contains all private/reserved IP ranges that should be blocked.
var privateRanges = []net.IPNet{
	{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
	{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)},
	{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)},
	{IP: net.ParseIP("127.0.0.0"), Mask: net.CIDRMask(8, 32)},
	{IP: net.ParseIP("169.254.0.0"), Mask: net.CIDRMask(16, 32)}, // link-local / cloud metadata
	{IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)},       // IPv6 loopback
	{IP: net.ParseIP("fc00::"), Mask: net.CIDRMask(7, 128)},      // IPv6 unique local
	{IP: net.ParseIP("fe80::"), Mask: net.CIDRMask(10, 128)},     // IPv6 link-local
}

// IsPrivateIP checks if an IP address is in a private/reserved range.
func IsPrivateIP(ip net.IP) bool {
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateURL checks that a URL doesn't point to private/internal networks.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("SSRF blocked: empty hostname")
	}

	// Resolve DNS to check actual IP.
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return fmt.Errorf("SSRF blocked: %q resolves to private IP %s", host, ip)
		}
	}
	return nil
}
