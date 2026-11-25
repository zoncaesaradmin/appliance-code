module appliance-code/services/channel-integration

go 1.26.4

require github.com/zoncaesaradmin/platformkit v0.1.0

require (
	appliance-code/sdk/golang/gen/messaging v0.0.0-00010101000000-000000000000
	appliance-code/sdk/golang/messaging v0.0.0-00010101000000-000000000000
	appliance-code/sdk/golang/servicehealth v0.0.0-00010101000000-000000000000
)

require (
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/nats-io/nats.go v1.47.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)

replace appliance-code/sdk/golang/gen/messaging => ../../sdk/golang/gen/messaging

replace appliance-code/sdk/golang/messaging => ../../sdk/golang/messaging

replace appliance-code/sdk/golang/servicehealth => ../../sdk/golang/servicehealth
