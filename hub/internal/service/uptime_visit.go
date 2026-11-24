package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// newUptimeClient is one client for every visit: a shared transport that
// keeps a bounded number of connections open, no redirects followed (a
// redirect is an answer), and on the hosted hub only public
// addresses.
func newUptimeClient(publicOnly bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if publicOnly {
		dialer.Control = refusePrivate
	}
	return &http.Client{
		Timeout: uptimeTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: uptimeTimeout,
			MaxIdleConns:          1024,
			MaxIdleConnsPerHost:   1,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// visit GETs a domain like a visitor: any answer below 500 is up, a 5xx, a
// timeout, a refused connection or a bad certificate is down. It reads at
// most a few KB of the body. The caller stamps when it ran.
func (u *uptimeRunner) visit(ctx context.Context, domain, path string) uptimeResult {
	var result uptimeResult
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, uptimeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.url(domain, path), nil)
	if err != nil {
		result.failure = "cannot visit " + domain + path
		return result
	}
	request.Header.Set("User-Agent", build.Name+"-uptime/"+build.Version)
	response, err := u.client.Do(request)
	result.responseMS = int(time.Since(start).Milliseconds())
	if err != nil {
		result.failure = uptimeFailure(err)
		return result
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, uptimeBodyLimit))
	result.statusCode = response.StatusCode
	result.up = response.StatusCode < 500
	if !result.up {
		result.failure = "HTTP " + strconv.Itoa(response.StatusCode)
	}
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		expiry := response.TLS.PeerCertificates[0].NotAfter.UTC()
		result.certExpiry = &expiry
	}
	return result
}

// uptimeFailure says in a few words why a visit failed.
func uptimeFailure(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var hostnameErr x509.HostnameError
	var netErr net.Error
	switch {
	case errors.Is(err, ErrPrivateAddress):
		return "the domain points at a private address"
	case errors.As(err, &dnsErr):
		return "DNS: no address for " + dnsErr.Name
	case errors.As(err, &hostnameErr):
		return "the HTTPS certificate is not for this domain"
	case errors.As(err, &certErr):
		return "the HTTPS certificate is not valid: " + clipText(certErr.Err.Error(), 120)
	case errors.As(err, &netErr) && netErr.Timeout():
		return fmt.Sprintf("no answer within %ds", int(uptimeTimeout.Seconds()))
	case strings.Contains(err.Error(), "connection refused"):
		return "connection refused"
	case strings.Contains(err.Error(), "connection reset"):
		return "connection reset"
	}
	return clipText(err.Error(), 160)
}

// ErrPrivateAddress refuses a connection to an address that is not on the
// public internet (AllowOnlyPublicAddresses).
var ErrPrivateAddress = errors.New("the address is private or local: moin only reaches the public internet")

// sharedNetwork is 100.64.0.0/10: carrier networks and Tailscale.
var sharedNetwork = netip.MustParsePrefix("100.64.0.0/10")

// refusePrivate stops a connection to loopback, private, link-local,
// shared or unspecified addresses, like 127.0.0.1, 10.0.0.5, 169.254.169.254
// or 0.0.0.0.
func refusePrivate(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || sharedNetwork.Contains(ip) {
		return ErrPrivateAddress
	}
	return nil
}
