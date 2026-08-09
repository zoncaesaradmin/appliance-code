package messaging

import "testing"

import gen "appliance-code/sdk/golang/gen/messaging"
import "google.golang.org/protobuf/proto"

func TestConnectRequiresURLAndName(t *testing.T) {
	if _, err := Connect(Options{Name: "test"}); err == nil {
		t.Fatal("expected URL validation")
	}
	if _, err := Connect(Options{URL: "nats://127.0.0.1:4222"}); err == nil {
		t.Fatal("expected name validation")
	}
}

func TestMarshalBuildsTypedOneof(t *testing.T) {
	wire, err := marshal("event.system.ready", "test", &gen.SystemEvent{Service: "test", State: gen.SystemEvent_READY})
	if err != nil {
		t.Fatal(err)
	}
	message := &gen.Message{}
	if err := proto.Unmarshal(wire, message); err != nil {
		t.Fatal(err)
	}
	if message.GetSystemEvent().GetService() != "test" {
		t.Fatalf("unexpected typed body: %+v", message.GetSystemEvent())
	}
}

func TestMarshalRejectsUntypedBody(t *testing.T) {
	if _, err := marshal("event.system.ready", "test", nil); err == nil {
		t.Fatal("expected nil protobuf body to be rejected")
	}
}
