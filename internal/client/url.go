package client

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// tailnet is the address range Tailscale gives devices (CGNAT, 100.64.0.0/10).
var tailnet = netip.MustParsePrefix("100.64.0.0/10")

// CheckURL refuses a control plane URL the token would cross the internet
// in plain text to. http is fine on this machine and on a tailnet, which
// Tailscale already encrypts; everywhere else it must be https.
func CheckURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%q is not a URL. Use one like https://ops.example.com", raw)
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if privateHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("%s sends your token unencrypted. Use https://%s, or plain http only on localhost or your tailnet", raw, parsed.Host)
	default:
		return fmt.Errorf("%q is not an http or https URL. Use one like https://ops.example.com", raw)
	}
}

// privateHost reports whether host is this machine or a tailnet address.
func privateHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".ts.net") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || tailnet.Contains(addr)
}
