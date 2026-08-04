package wifiap

import "testing"

func TestDeriveSSIDPrefersShortHostname(t *testing.T) {
	ssid, err := DeriveSSID("Kitchen-Box.example.internal")
	if err != nil {
		t.Fatal(err)
	}
	if ssid != "kitchen-box-AP" {
		t.Fatalf("ssid = %q, want kitchen-box-AP", ssid)
	}
}

func TestDeriveSSIDTruncatesLongNames(t *testing.T) {
	base := "this-is-a-very-long-appliance-hostname-value"
	ssid, err := DeriveSSID(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(ssid) > maxSSIDLen {
		t.Fatalf("ssid length %d exceeds %d: %q", len(ssid), maxSSIDLen, ssid)
	}
	if ssid[len(ssid)-3:] != "-AP" {
		t.Fatalf("ssid = %q, want -AP suffix", ssid)
	}
}

func TestValidatePSK(t *testing.T) {
	if err := ValidatePSK("short"); err == nil {
		t.Fatal("expected short psk to fail")
	}
	if err := ValidatePSK("long-enough-secret"); err != nil {
		t.Fatalf("expected valid psk, got %v", err)
	}
}
