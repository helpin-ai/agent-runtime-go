package sdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	DefaultNATSStreamName      = "AGENT_RUNTIME_EVENTS"
	DefaultNATSStreamSubject   = "agent-runtime.events.>"
	DefaultNATSSubjectTemplate = "agent-runtime.events.{app_id}.{run_id}.{event_type}"
)

var (
	ErrRetryEvent = errors.New("retry agent-runtime event")
	ErrDropEvent  = errors.New("drop agent-runtime event")
)

type NATSConsumerConfig struct {
	URL            string
	ClientName     string
	JetStream      nats.JetStreamContext
	Stream         string
	StreamSubjects []string
	AppID          string
	Durable        string
	Subject        string
	FetchBatch     int
	MaxWait        time.Duration
	AckWait        time.Duration
	MaxDeliver     int
	BackOff        []time.Duration
	MaxAckPending  int
	EnsureStream   bool
	Logger         *slog.Logger
}

type NATSConsumer struct {
	cfg NATSConsumerConfig
}

type NATSEventHandler = EventHandler

func NewNATSConsumer(cfg NATSConsumerConfig) *NATSConsumer {
	return &NATSConsumer{cfg: cfg.withDefaults()}
}

func (c *NATSConsumer) Run(ctx context.Context, handler NATSEventHandler) error {
	if c == nil {
		return nil
	}
	cfg := c.cfg.withDefaults()
	if handler == nil {
		return fmt.Errorf("event handler is required")
	}
	js, closeFn, err := cfg.jetStream()
	if err != nil {
		return err
	}
	defer closeFn()
	if cfg.EnsureStream {
		if err := EnsureNATSStream(js, cfg.Stream, cfg.StreamSubjects); err != nil {
			return err
		}
	}
	sub, err := cfg.subscribe(ctx, js)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return sub.Drain()
		default:
		}
		msgs, err := sub.Fetch(cfg.FetchBatch, nats.MaxWait(cfg.MaxWait))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				continue
			}
			cfg.log().ErrorContext(ctx, "agent runtime nats consumer fetch failed", "stream", cfg.Stream, "durable", cfg.Durable, "error", err)
			continue
		}
		for _, msg := range msgs {
			cfg.process(ctx, msg, handler)
		}
	}
}

func (cfg NATSConsumerConfig) withDefaults() NATSConsumerConfig {
	if strings.TrimSpace(cfg.ClientName) == "" {
		cfg.ClientName = "agent-runtime-sdk-consumer"
	}
	if strings.TrimSpace(cfg.Stream) == "" {
		cfg.Stream = DefaultNATSStreamName
	}
	if len(cfg.StreamSubjects) == 0 {
		cfg.StreamSubjects = []string{DefaultNATSStreamSubject}
	}
	if strings.TrimSpace(cfg.Subject) == "" {
		cfg.Subject = AppEventSubject(cfg.AppID)
	}
	if strings.TrimSpace(cfg.Durable) == "" {
		cfg.Durable = "agent-runtime-projection"
	}
	if cfg.FetchBatch <= 0 {
		cfg.FetchBatch = 8
	}
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = 5 * time.Second
	}
	if cfg.AckWait <= 0 {
		cfg.AckWait = 45 * time.Second
	}
	if cfg.MaxDeliver <= 0 {
		cfg.MaxDeliver = 5
	}
	if len(cfg.BackOff) == 0 {
		cfg.BackOff = []time.Duration{
			5 * time.Second,
			15 * time.Second,
			45 * time.Second,
			2 * time.Minute,
			5 * time.Minute,
		}
	}
	if cfg.MaxAckPending <= 0 {
		cfg.MaxAckPending = 64
	}
	cfg.EnsureStream = cfg.EnsureStream || (cfg.JetStream == nil && strings.TrimSpace(cfg.URL) != "")
	return cfg
}

func (cfg NATSConsumerConfig) jetStream() (nats.JetStreamContext, func(), error) {
	if cfg.JetStream != nil {
		return cfg.JetStream, func() {}, nil
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, nil, fmt.Errorf("nats URL or JetStream context is required")
	}
	nc, err := nats.Connect(strings.TrimSpace(cfg.URL),
		nats.Name(strings.TrimSpace(cfg.ClientName)),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, nil, err
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	return js, nc.Close, nil
}

func (cfg NATSConsumerConfig) subscribe(ctx context.Context, js nats.JetStreamContext) (*nats.Subscription, error) {
	if js == nil {
		return nil, fmt.Errorf("jetstream context is nil")
	}
	subject := strings.TrimSpace(cfg.Subject)
	if subject == "" {
		subject = AppEventSubject(cfg.AppID)
	}
	if info, err := js.ConsumerInfo(cfg.Stream, cfg.Durable); err == nil && info != nil {
		if strings.TrimSpace(info.Config.FilterSubject) != subject {
			if err := js.DeleteConsumer(cfg.Stream, cfg.Durable); err != nil {
				return nil, err
			}
		}
	}
	if sub, err := js.PullSubscribe(subject, cfg.Durable, nats.Bind(cfg.Stream, cfg.Durable)); err == nil {
		return sub, nil
	}
	consumerCfg := &nats.ConsumerConfig{
		Durable:       strings.TrimSpace(cfg.Durable),
		FilterSubject: subject,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       cfg.AckWait,
		MaxDeliver:    cfg.MaxDeliver,
		BackOff:       cfg.BackOff,
		DeliverPolicy: nats.DeliverAllPolicy,
		MaxAckPending: cfg.MaxAckPending,
	}
	if _, err := js.AddConsumer(cfg.Stream, consumerCfg); err != nil {
		return nil, err
	}
	cfg.log().InfoContext(ctx, "agent runtime nats consumer created", "stream", cfg.Stream, "durable", cfg.Durable, "subject", subject)
	return js.PullSubscribe(subject, cfg.Durable, nats.Bind(cfg.Stream, cfg.Durable))
}

func (cfg NATSConsumerConfig) process(ctx context.Context, msg *nats.Msg, handler NATSEventHandler) {
	event, err := ParseEventEnvelope(msg.Data)
	if err != nil {
		cfg.log().WarnContext(ctx, "agent runtime nats consumer invalid payload", "subject", msg.Subject, "error", err)
		_ = msg.Ack()
		return
	}
	err = handler(ctx, *event)
	switch {
	case err == nil:
		_ = msg.Ack()
	case errors.Is(err, ErrDropEvent):
		_ = msg.Ack()
	case errors.Is(err, ErrRetryEvent):
		deliveries := cfg.deliveries(msg)
		if deliveries >= uint64(cfg.MaxDeliver) {
			cfg.log().ErrorContext(ctx, "agent runtime nats consumer dropping event after max deliveries",
				"runtime_run_id", event.RunID,
				"host_run_id", event.HostRunID,
				"event_type", event.Type,
			)
			_ = msg.Ack()
			return
		}
		_ = msg.NakWithDelay(cfg.retryDelay(deliveries))
	default:
		deliveries := cfg.deliveries(msg)
		cfg.log().ErrorContext(ctx, "agent runtime nats consumer handler failed",
			"runtime_run_id", event.RunID,
			"host_run_id", event.HostRunID,
			"event_type", event.Type,
			"error", err,
		)
		if deliveries >= uint64(cfg.MaxDeliver) {
			_ = msg.Ack()
			return
		}
		_ = msg.NakWithDelay(cfg.retryDelay(deliveries))
	}
}

func (cfg NATSConsumerConfig) deliveries(msg *nats.Msg) uint64 {
	meta, _ := msg.Metadata()
	if meta == nil {
		return 1
	}
	return meta.NumDelivered
}

func (cfg NATSConsumerConfig) retryDelay(deliveries uint64) time.Duration {
	if len(cfg.BackOff) == 0 {
		return 5 * time.Second
	}
	if deliveries == 0 {
		deliveries = 1
	}
	index := int(deliveries - 1)
	if index >= len(cfg.BackOff) {
		index = len(cfg.BackOff) - 1
	}
	return cfg.BackOff[index]
}

func (cfg NATSConsumerConfig) log() *slog.Logger {
	if cfg.Logger != nil {
		return cfg.Logger
	}
	return slog.Default()
}

func EnsureNATSStream(js nats.JetStreamContext, stream string, subjects []string) error {
	if js == nil {
		return fmt.Errorf("jetstream context is nil")
	}
	stream = strings.TrimSpace(stream)
	if stream == "" {
		stream = DefaultNATSStreamName
	}
	if len(subjects) == 0 {
		subjects = []string{DefaultNATSStreamSubject}
	}
	cfg := &nats.StreamConfig{
		Name:       stream,
		Subjects:   subjects,
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		Discard:    nats.DiscardOld,
		Duplicates: 2 * time.Minute,
		MaxAge:     7 * 24 * time.Hour,
		MaxBytes:   512 * 1024 * 1024,
	}
	if _, err := js.StreamInfo(stream); err != nil {
		if !errors.Is(err, nats.ErrStreamNotFound) {
			return err
		}
		_, err = js.AddStream(cfg)
		return err
	}
	return nil
}

func AppEventSubject(appID string) string {
	return "agent-runtime.events." + NATSAppToken(appID) + ".>"
}

func RenderNATSSubject(template string, event EventEnvelope) string {
	template = strings.TrimSpace(template)
	if template == "" {
		template = DefaultNATSSubjectTemplate
	}
	replacer := strings.NewReplacer(
		"{app_id}", NATSAppToken(event.AppID),
		"{run_id}", NATSToken(event.RunID),
		"{event_type}", NATSToken(event.Type),
		"{type}", NATSToken(event.Type),
	)
	return replacer.Replace(template)
}

func NATSToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	value = strings.NewReplacer(" ", "_", "\t", "_", "\n", "_", "\r", "_", "/", "_", "\\", "_", "*", "_", ">", "_").Replace(value)
	return value
}

func NATSAppToken(value string) string {
	return strings.ReplaceAll(NATSToken(value), ".", "_")
}
