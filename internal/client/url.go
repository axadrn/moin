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

// tailnet6 is Tailscale's IPv6 range, which MagicDNS answers next to it.
var tailnet6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

// CheckURL refuses a control plane URL the token would cross the internet
// in plain text to. http is fine on this machine and on a tailnet, which
// Tailscale already encrypts; everywhere else it must be https.
func CheckURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%q is not a URL. Use one like https://moin.example.com", raw)
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if privateHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("plain http sends your token unencrypted to %s. Use https://%s, or http only on localhost or your tailnet", parsed.Host, parsed.Host)
	default:
		return fmt.Errorf("%q is not an http or https URL. Use one like https://moin.example.com", raw)
	}
}

// lookupHost resolves a name; tests replace it.
var lookupHost = net.LookupHost

// privateHost reports whether host is this machine or a tailnet address. A
// short name like host-1 counts when Tailscale's MagicDNS resolves it to
// tailnet addresses only.
func privateHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".ts.net") {
		return true
	}
	if privateAddress(host) {
		return true
	}
	if net.ParseIP(host) != nil || strings.Contains(host, ".") {
		return false
	}
	addresses, err := lookupHost(host)
	if err != nil || len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if !privateAddress(address) {
			return false
		}
	}
	return true
}

func privateAddress(address string) bool {
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || tailnet.Contains(addr) || tailnet6.Contains(addr)
}
