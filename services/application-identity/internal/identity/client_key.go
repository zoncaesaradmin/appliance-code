package identity

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedClientKey accepts forwarded addresses only from configured proxies.
// Trust only dedicated proxy address ranges: clients within those ranges can
// otherwise impersonate forwarding hops. Network policy must prevent direct
// connections from other workloads that can use the proxy addresses.
func TrustedClientKey(trusted []netip.Prefix) (ClientKey, error) {
	for _, prefix := range trusted {
		if !prefix.IsValid() {
			return nil, errors.New("invalid trusted proxy prefix")
		}
	}
	proxies := append([]netip.Prefix(nil), trusted...)
	isTrusted := func(addr netip.Addr) bool {
		for _, prefix := range proxies {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	return func(r *http.Request) (string, error) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return "", errors.New("invalid peer address")
		}
		peer, err := netip.ParseAddr(host)
		if err != nil || peer.Zone() != "" {
			return "", errors.New("invalid peer address")
		}
		peer = peer.Unmap()
		if !isTrusted(peer) {
			return peer.String(), nil
		}
		forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
		if forwarded == "" || len(forwarded) > 4096 {
			return "", errors.New("missing or invalid forwarded client")
		}
		hops := strings.Split(forwarded, ",")
		if len(hops) > 32 {
			return "", errors.New("too many forwarded hops")
		}
		for i := len(hops) - 1; i >= 0; i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil || addr.Zone() != "" {
				return "", errors.New("invalid forwarded address")
			}
			addr = addr.Unmap()
			if !isTrusted(addr) {
				return addr.String(), nil
			}
		}
		return "", errors.New("forwarded chain contains no untrusted client")
	}, nil
}
