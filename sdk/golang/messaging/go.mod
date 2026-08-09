module appliance-code/sdk/golang/messaging

go 1.26.4

require (
	appliance-code/sdk/golang/gen/messaging v0.0.0-00010101000000-000000000000
	github.com/nats-io/nats.go v1.47.0
	google.golang.org/protobuf v1.33.0
)

require (
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
)

replace appliance-code/sdk/golang/gen/messaging => ../gen/messaging
