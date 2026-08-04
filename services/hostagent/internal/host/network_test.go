package host

import "testing"

func TestIsWifiAPManagementIPv4(t *testing.T) {
	if !IsWifiAPManagementIPv4("10.42.0.1") {
		t.Fatal("expected management AP host address")
	}
	if !IsWifiAPManagementIPv4("10.42.0.25") {
		t.Fatal("expected AP client range address")
	}
	if IsWifiAPManagementIPv4("192.168.1.151") {
		t.Fatal("LAN address must not match AP range")
	}
	if IsWifiAPManagementIPv4("10.42.1.1") {
		t.Fatal("only 10.42.0.0/24 is reserved for management AP")
	}
}

func TestSummarizeNetworkPrefersEthernetLAN(t *testing.T) {
	status := summarizeNetwork([]NetworkLink{
		{Name: "wlan0", Kind: "wifi", State: "up", Role: "management-ap", IPv4Addresses: []string{"10.42.0.1"}},
		{Name: "enp1s0", Kind: "ethernet", State: "up", Role: "lan", IPv4Addresses: []string{"192.168.1.151"}},
		{Name: "wlp2s0", Kind: "wifi", State: "up", Role: "lan", IPv4Addresses: []string{"192.168.1.200"}},
	})
	if status.PrimaryLANIPv4 != "192.168.1.151" {
		t.Fatalf("primaryLANIPv4 = %q, want 192.168.1.151", status.PrimaryLANIPv4)
	}
	if status.PrimaryLANSource != "ethernet" {
		t.Fatalf("primaryLANSource = %q, want ethernet", status.PrimaryLANSource)
	}
	if !status.Ethernet.Present || !status.Ethernet.Enabled {
		t.Fatalf("ethernet status = %+v", status.Ethernet)
	}
	if !status.Wifi.Present || !status.Wifi.Enabled {
		t.Fatalf("wifi status = %+v", status.Wifi)
	}
	if !status.WifiAP.Present || !status.WifiAP.Enabled {
		t.Fatalf("wifiAP status = %+v", status.WifiAP)
	}
	if got := status.WifiAP.IPv4Addresses; len(got) != 1 || got[0] != "10.42.0.1" {
		t.Fatalf("wifiAP addresses = %v", got)
	}
}

func TestSummarizeNetworkWifiOnly(t *testing.T) {
	status := summarizeNetwork([]NetworkLink{
		{Name: "wlp2s0", Kind: "wifi", State: "up", Role: "lan", IPv4Addresses: []string{"10.0.0.5"}},
	})
	if status.PrimaryLANIPv4 != "10.0.0.5" || status.PrimaryLANSource != "wifi" {
		t.Fatalf("primary = %s/%s", status.PrimaryLANIPv4, status.PrimaryLANSource)
	}
	if status.Ethernet.Present {
		t.Fatal("ethernet should be absent")
	}
}

func TestShouldSkipInterface(t *testing.T) {
	for _, name := range []string{"lo", "docker0", "cni0", "flannel.1", "vethabc", "cali123"} {
		if !shouldSkipInterface(name) {
			t.Fatalf("expected skip %q", name)
		}
	}
	for _, name := range []string{"eth0", "enp1s0", "wlan0", "wlp3s0"} {
		if shouldSkipInterface(name) {
			t.Fatalf("did not expect skip %q", name)
		}
	}
}
