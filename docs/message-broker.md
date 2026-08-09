# Appliance Message Broker

The `appliance-message-broker` chart deploys the always-on internal NATS
JetStream service in the `control` namespace. It has no public route or
capability gate. Its file-backed state is kept in a dedicated PVC so durable
consumer delivery survives pod restarts and signed bundle upgrades.

Services use `sdk/golang/messaging`; direct NATS imports are not part of the
service contract. Durable consumers use explicit names and acknowledge a
message after successful handling. Replay is requested by starting the same
durable consumer again, using JetStream's persisted delivery state.

The complete wire contract is one file, `proto/messaging.proto`. It defines a
typed `Message` with a protobuf `oneof` body for operational events, workflow
events and commands, audit events, system events, service commands, and
replies. Generated Go bindings live separately under
`sdk/golang/gen/messaging`; the SDK accepts only those generated payload types
and publishes protobuf wire bytes, so an arbitrary byte payload cannot bypass
the schema.
