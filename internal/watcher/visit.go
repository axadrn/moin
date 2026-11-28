package watcher

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// visitTimeout is how long a visit waits for the answer.
const visitTimeout = 10 * time.Second

// Visitor visits domains from outside, like a person with a browser. At
// most a few visits run at once: moin cloud shares one Visitor between
// all its watchers.
type Visitor struct {
	client *http.Client
	// slots holds one value for each visit that runs now.
	slots chan struct{}
}

// NewVisitor is a visitor that runs at most atOnce visits at the same time.
func NewVisitor(atOnce int) *Visitor {
	return &Visitor{client: visitClient(), slots: make(chan struct{}, atOnce)}
}

// Visit GETs a domain like a visitor and says why it failed, or "" when it
// is up: any answer below 500 is up; a 5xx, a timeout, a refused
// connection or a bad certificate is down. It waits for a free slot first.
func (v *Visitor) Visit(ctx context.Context, domain string) string {
	select {
	case v.slots <- struct{}{}:
		defer func() { <-v.slots }()
	case <-ctx.Done():
		return "stopped"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		return "not a domain"
	}
	request.Header.Set("User-Agent", build.Name+"-watcher/"+build.Version)
	response, err := v.client.Do(request)
	if err != nil {
		return visitFailure(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode >= 500 {
		return "HTTP " + strconv.Itoa(response.StatusCode)
	}
	return ""
}

// visitClient visits like a new visitor each time: a new connection, no
// redirects followed (a redirect is an answer), and only public addresses,
// so a report cannot make the watcher knock on its own network.
func visitClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: RefusePrivate}
	return &http.Client{
		Timeout: visitTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: visitTimeout,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// errPrivate is a visit to an address that is not on the public internet.
var errPrivate = errors.New("the domain points at a private address")

// sharedNetwork is 100.64.0.0/10: carrier networks and Tailscale.
var sharedNetwork = netip.MustParsePrefix("100.64.0.0/10")

// RefusePrivate stops a connection to loopback, private, link-local,
// shared or unspecified addresses. It is the Control of a net.Dialer: it
// runs after DNS, so a name that points inside cannot slip past it.
func RefusePrivate(network, address string, _ syscall.RawConn) error {
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
		return errPrivate
	}
	return nil
}

// visitFailure says in a few words why a visit got no answer.
func visitFailure(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var netErr net.Error
	var urlErr *url.Error
	switch {
	case errors.Is(err, errPrivate):
		return errPrivate.Error()
	case errors.As(err, &dnsErr):
		return "DNS has no address for " + dnsErr.Name
	case errors.As(err, &certErr):
		return "the HTTPS certificate is not valid"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "no answer within " + visitTimeout.String()
	case errors.As(err, &urlErr):
		return clip(urlErr.Err.Error(), maxError)
	}
	return clip(err.Error(), maxError)
}
