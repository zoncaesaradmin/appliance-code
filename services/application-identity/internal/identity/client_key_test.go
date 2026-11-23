package identity

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestTrustedClientKey(t *testing.T) {
	key, err := TrustedClientKey([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, remote, forwarded, want string
		fail                          bool
	}{
		{"direct ignores forged header", "198.51.100.1:1234", "203.0.113.8", "198.51.100.1", false},
		{"proxy chain", "10.1.0.1:1234", "203.0.113.9, 198.51.100.8, 10.2.0.1", "198.51.100.8", false},
		{"ipv6", "[fd00::1]:1234", "2001:db8::1", "2001:db8::1", false},
		{"mapped IPv4", "[::ffff:198.51.100.1]:1234", "", "198.51.100.1", false},
		{"trusted peer missing header", "10.1.0.1:1234", "", "", true},
		{"invalid forwarding", "10.1.0.1:1234", "broken", "", true},
		{"all trusted hops", "10.1.0.1:1234", "10.2.0.1", "", true},
		{"invalid peer", "not-an-address", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://app.example/", nil)
			r.RemoteAddr = tc.remote
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			got, err := key(r)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("got %q %v, want %q fail=%v", got, err, tc.want, tc.fail)
			}
		})
	}
}
