package agent

import (
	"net"
	"net/netip"
)

// publicAddresses are the public IPv4 and IPv6 address of this server, as
// far as it knows them: the source address it would use to reach the
// internet. UDP sends nothing until written to, so this costs no traffic.
// A server behind NAT has no public address of its own and reports none.
func publicAddresses() []string {
	var addresses []string
	for _, probe := range [][2]string{{"udp4", "1.1.1.1:53"}, {"udp6", "[2606:4700:4700::1111]:53"}} {
		conn, err := net.Dial(probe[0], probe[1])
		if err != nil {
			continue
		}
		local, ok := conn.LocalAddr().(*net.UDPAddr)
		conn.Close()
		if !ok {
			continue
		}
		address, ok := netip.AddrFromSlice(local.IP)
		if ok && isPublic(address.Unmap()) {
			addresses = append(addresses, address.Unmap().String())
		}
	}
	return addresses
}

func isPublic(address netip.Addr) bool {
	return address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() && !isCGNAT(address)
}

// isCGNAT reports 100.64.0.0/10, the shared space of carrier NAT and
// Tailscale.
func isCGNAT(address netip.Addr) bool {
	return netip.MustParsePrefix("100.64.0.0/10").Contains(address)
}
