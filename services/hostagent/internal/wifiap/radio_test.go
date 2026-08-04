package wifiap

import "testing"

func TestParseIWDev(t *testing.T) {
	out := `phy#0
	Interface wlan0
		ifindex 3
		wdev 0x1
		addr 00:11:22:33:44:55
		type managed
		wiphy 0
	Interface wlan1
		type AP
		wiphy 0
`
	cands := parseIWDev(out)
	if len(cands) != 2 {
		t.Fatalf("got %d candidates: %+v", len(cands), cands)
	}
	if cands[0].Iface != "wlan0" || cands[0].Type != "managed" || cands[0].Phy != "phy0" {
		t.Fatalf("unexpected first candidate: %+v", cands[0])
	}
	if cands[1].Iface != "wlan1" || cands[1].Type != "ap" {
		t.Fatalf("unexpected second candidate: %+v", cands[1])
	}
}

func TestPhysWithAP(t *testing.T) {
	list := `Wiphy phy0
	Supported interface modes:
		 * managed
		 * AP
Wiphy phy1
	Supported interface modes:
		 * managed
`
	m := physWithAP(list)
	if !m["phy0"] {
		t.Fatal("phy0 should support AP")
	}
	if m["phy1"] {
		t.Fatal("phy1 should not support AP")
	}
}
