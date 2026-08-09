// Package messaging provides the only supported appliance producer and
// consumer interface. Services must not import the underlying NATS client.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	gen "appliance-code/sdk/golang/gen/messaging"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var DefaultStreams = []StreamSpec{
	{Name: "operational", Subjects: []string{"event.operational.>"}},
	{Name: "workflow", Subjects: []string{"event.workflow.>", "command.workflow.>"}},
	{Name: "audit", Subjects: []string{"event.audit.>"}},
	{Name: "system", Subjects: []string{"event.system.>", "system.>"}},
}

type StreamSpec struct {
	Name     string
	Subjects []string
	MaxAge   time.Duration
}

type Options struct {
	URL       string
	Name      string
	Timeout   time.Duration
	JetStream bool
}

type Client struct {
	conn *nats.Conn
	js   nats.JetStreamContext
}

func Connect(opts Options) (*Client, error) {
	if opts.URL == "" {
		return nil, errors.New("messaging: URL is required")
	}
	if opts.Name == "" {
		return nil, errors.New("messaging: Name is required")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	conn, err := nats.Connect(opts.URL, nats.Name(opts.Name), nats.Timeout(opts.Timeout))
	if err != nil {
		return nil, fmt.Errorf("messaging: connect: %w", err)
	}
	client := &Client{conn: conn}
	if opts.JetStream {
		client.js, err = conn.JetStream()
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("messaging: jetstream: %w", err)
		}
	}
	return client, nil
}

func (c *Client) Close() {
	if c != nil && c.conn != nil {
		c.conn.Close()
	}
}

func (c *Client) EnsureStream(ctx context.Context, spec StreamSpec) error {
	if c.js == nil {
		return errors.New("messaging: stream management requires JetStream")
	}
	if spec.Name == "" || len(spec.Subjects) == 0 {
		return errors.New("messaging: stream name and subjects are required")
	}
	if spec.MaxAge == 0 {
		spec.MaxAge = 7 * 24 * time.Hour
	}
	_, err := c.js.AddStream(&nats.StreamConfig{Name: spec.Name, Subjects: spec.Subjects, Storage: nats.FileStorage, MaxAge: spec.MaxAge, Retention: nats.LimitsPolicy}, nats.Context(ctx))
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return fmt.Errorf("messaging: ensure stream %s: %w", spec.Name, err)
	}
	return nil
}

// Publish accepts only one of the generated protobuf payload messages. The
// SDK constructs the Message oneof, so callers cannot publish an untyped body.
func (c *Client) Publish(ctx context.Context, subject, producer string, body proto.Message) error {
	wire, err := marshal(subject, producer, body)
	if err != nil {
		return err
	}
	if c.js != nil {
		_, err = c.js.Publish(subject, wire, nats.Context(ctx))
	} else {
		err = c.conn.Publish(subject, wire)
	}
	return err
}

type Message struct {
	Wire *gen.Message
	Ack  func() error
}

func (c *Client) Subscribe(ctx context.Context, subject, durable string, handler func(context.Context, Message) error) error {
	if c.js == nil {
		return errors.New("messaging: durable subscribe requires JetStream")
	}
	if durable == "" {
		return errors.New("messaging: durable name is required")
	}
	consumer, err := c.js.SubscribeSync(subject, nats.Durable(durable), nats.ManualAck())
	if err != nil {
		return fmt.Errorf("messaging: subscribe: %w", err)
	}
	defer consumer.Unsubscribe()
	for {
		msg, err := consumer.NextMsg(time.Second)
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					continue
				}
			}
			return err
		}
		wire := &gen.Message{}
		if err := proto.Unmarshal(msg.Data, wire); err != nil || wire.GetBody() == nil {
			_ = msg.Term()
			continue
		}
		if err := handler(ctx, Message{Wire: wire, Ack: func() error { return msg.Ack() }}); err != nil {
			_ = msg.Nak()
			return err
		}
		if err := msg.Ack(); err != nil {
			return err
		}
	}
}

func (c *Client) Request(ctx context.Context, subject, producer string, body proto.Message) (*gen.Reply, error) {
	wire, err := marshal(subject, producer, body)
	if err != nil {
		return nil, err
	}
	msg, err := c.conn.RequestWithContext(ctx, subject, wire)
	if err != nil {
		return nil, err
	}
	response := &gen.Message{}
	if err := proto.Unmarshal(msg.Data, response); err != nil {
		return nil, fmt.Errorf("messaging: decode reply: %w", err)
	}
	reply := response.GetReply()
	if reply == nil {
		return nil, errors.New("messaging: response is not a reply")
	}
	return reply, nil
}

func marshal(subject, producer string, body proto.Message) ([]byte, error) {
	if subject == "" || producer == "" {
		return nil, errors.New("messaging: subject and producer are required")
	}
	if body == nil {
		return nil, errors.New("messaging: protobuf body is required")
	}
	message := &gen.Message{Header: &gen.Header{Id: nats.NewInbox(), Subject: subject, Producer: producer, CreatedAt: timestamppb.Now()}}
	switch value := body.(type) {
	case *gen.OperationalEvent:
		message.Body = &gen.Message_OperationalEvent{OperationalEvent: value}
	case *gen.WorkflowEvent:
		message.Body = &gen.Message_WorkflowEvent{WorkflowEvent: value}
	case *gen.AuditEvent:
		message.Body = &gen.Message_AuditEvent{AuditEvent: value}
	case *gen.SystemEvent:
		message.Body = &gen.Message_SystemEvent{SystemEvent: value}
	case *gen.WorkflowCommand:
		message.Body = &gen.Message_WorkflowCommand{WorkflowCommand: value}
	case *gen.ServiceCommand:
		message.Body = &gen.Message_ServiceCommand{ServiceCommand: value}
	case *gen.Reply:
		message.Body = &gen.Message_Reply{Reply: value}
	default:
		return nil, fmt.Errorf("messaging: unsupported protobuf body %T", body)
	}
	wire, err := proto.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("messaging: marshal protobuf: %w", err)
	}
	return wire, nil
}
