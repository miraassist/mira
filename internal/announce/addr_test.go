package announce

import (
	"net"
	"testing"
)

// Only plain IPv4 addresses are announced: an IPv6 address, usually a
// temporary privacy one, would be gone by the time Home Assistant uses it.
func TestOnlyIPv4IsAnnounced(t *testing.T) {
	for ip, want := range map[string]bool{
		"192.168.178.186":                        true,
		"10.0.0.7":                               true,
		"169.254.1.2":                            false, // link local
		"127.0.0.1":                              false,
		"fdf4:27c6:8ba:0:4298:2414:e338:b4cc":    false, // temporary ULA address
		"2001:16b8:a6be:3000:a15f:c564:3a27:62b": false,
		"fe80::1":                                false,
		"::1":                                    false,
	} {
		if got := announcable(net.ParseIP(ip)); got != want {
			t.Errorf("announcable(%s) = %v, want %v", ip, got, want)
		}
	}
}
