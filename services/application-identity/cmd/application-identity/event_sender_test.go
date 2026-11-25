package main

import "testing"

func TestSelectConfiguredChannelsFiltersUnsupportedRequests(t *testing.T) {
	got := selectConfiguredChannels([]string{"sms", "WhatsApp", "whatsapp"}, []string{"whatsapp"})
	if len(got) != 1 || got[0] != "whatsapp" {
		t.Fatalf("selected channels = %#v", got)
	}
	if got := selectConfiguredChannels([]string{"sms"}, []string{"whatsapp"}); len(got) != 0 {
		t.Fatalf("unsupported channel was selected: %#v", got)
	}
}
