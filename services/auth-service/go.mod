module appliance-code/services/auth-service

go 1.26.0

require (
	appliance-code/sdk/golang/gen/messaging v0.0.0-00010101000000-000000000000
	appliance-code/sdk/golang/messaging v0.0.0-00010101000000-000000000000
	appliance-code/sdk/golang/servicehealth v0.0.0-00010101000000-000000000000
	github.com/jackc/pgx/v5 v5.13.0
	google.golang.org/protobuf v1.33.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/nats-io/nats.go v1.47.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

replace appliance-code/sdk/golang/gen/messaging => ../../sdk/golang/gen/messaging

replace appliance-code/sdk/golang/messaging => ../../sdk/golang/messaging

replace appliance-code/sdk/golang/servicehealth => ../../sdk/golang/servicehealth
