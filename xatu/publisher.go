package xatu

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/creasty/defaults"
	"github.com/ethpandaops/rpc-snooper/utils"
	"github.com/ethpandaops/xatu/pkg/output"
	"github.com/ethpandaops/xatu/pkg/output/http"
	"github.com/ethpandaops/xatu/pkg/output/stdout"
	xatuOutput "github.com/ethpandaops/xatu/pkg/output/xatu"
	"github.com/ethpandaops/xatu/pkg/processor"
	xatu "github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/sirupsen/logrus"
)

// Sink configuration defaults.
const (
	defaultMaxQueueSize       = 51200
	defaultMaxExportBatchSize = 512
	defaultBatchTimeout       = 5 * time.Second
	defaultExportTimeout      = 30 * time.Second
)

// Publisher manages event sinks and publishes decorated events.
type Publisher interface {
	// Start initializes all sinks.
	Start(ctx context.Context) error

	// Stop gracefully shuts down all sinks.
	Stop(ctx context.Context) error

	// Publish sends a decorated event to all sinks.
	Publish(ctx context.Context, event *xatu.DecoratedEvent) error

	// ClientMeta returns the base client metadata for events.
	ClientMeta() *xatu.ClientMeta
}

type publisher struct {
	config *Config
	log    logrus.FieldLogger
	sinks  []output.Sink

	mu sync.RWMutex
}

// NewPublisher creates a new Publisher instance.
func NewPublisher(config *Config, log logrus.FieldLogger) Publisher {
	return &publisher{
		config: config,
		log:    log.WithField("component", "xatu_publisher"),
		sinks:  make([]output.Sink, 0),
	}
}

// Start initializes all configured sinks.
func (p *publisher) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, outConfig := range p.config.Outputs {
		sink, err := p.createSink(outConfig, i)
		if err != nil {
			return fmt.Errorf("failed to create sink %d (%s): %w", i, outConfig.Type, err)
		}

		if err := sink.Start(ctx); err != nil {
			return fmt.Errorf("failed to start sink %d (%s): %w", i, outConfig.Type, err)
		}

		p.sinks = append(p.sinks, sink)
		p.log.WithFields(logrus.Fields{
			"type":    outConfig.Type,
			"address": outConfig.Address,
		}).Info("started xatu sink")
	}

	return nil
}

// Stop gracefully shuts down all sinks.
func (p *publisher) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error

	for _, sink := range p.sinks {
		if err := sink.Stop(ctx); err != nil {
			p.log.WithError(err).WithField("sink", sink.Name()).Warn("failed to stop sink")
			lastErr = err
		}
	}

	p.sinks = nil

	return lastErr
}

// Publish sends a decorated event to all sinks.
func (p *publisher) Publish(ctx context.Context, event *xatu.DecoratedEvent) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var lastErr error

	for _, sink := range p.sinks {
		if err := sink.HandleNewDecoratedEvent(ctx, event); err != nil {
			p.log.WithError(err).WithField("sink", sink.Name()).Error("failed to publish event")
			lastErr = err
		}
	}

	return lastErr
}

// ClientMeta returns the base client metadata for events.
func (p *publisher) ClientMeta() *xatu.ClientMeta {
	return &xatu.ClientMeta{
		Name:           p.config.Name,
		Version:        utils.GetBuildVersion(),
		Implementation: "rpc-snooper",
		Labels:         p.config.Labels,
		ModuleName:     xatu.ModuleName_EL_MIMICRY,
	}
}

//nolint:ireturn // Interface return is intentional for sink abstraction
func (p *publisher) createSink(outConfig OutputConfig, index int) (output.Sink, error) {
	name := fmt.Sprintf("%s-%d", outConfig.Type, index)
	filterConfig := &xatu.EventFilterConfig{}
	shippingMethod := processor.ShippingMethodAsync

	switch outConfig.Type {
	case OutputTypeStdout:
		conf := &stdout.Config{}
		if err := defaults.Set(conf); err != nil {
			return nil, fmt.Errorf("failed to set stdout defaults: %w", err)
		}

		return stdout.New(name, conf, p.log.WithField("sink", name), filterConfig, shippingMethod)

	case OutputTypeHTTP:
		conf := &http.Config{
			Address:            outConfig.Address,
			Headers:            p.config.Headers,
			MaxQueueSize:       defaultMaxQueueSize,
			BatchTimeout:       defaultBatchTimeout,
			ExportTimeout:      defaultExportTimeout,
			MaxExportBatchSize: defaultMaxExportBatchSize,
		}
		if err := defaults.Set(conf); err != nil {
			return nil, fmt.Errorf("failed to set http defaults: %w", err)
		}

		return http.New(name, conf, p.log.WithField("sink", name), filterConfig, shippingMethod)

	case OutputTypeXatu:
		conf := &xatuOutput.Config{
			Address:            outConfig.Address,
			TLS:                p.config.TLS,
			Headers:            p.config.Headers,
			MaxQueueSize:       defaultMaxQueueSize,
			BatchTimeout:       defaultBatchTimeout,
			ExportTimeout:      defaultExportTimeout,
			MaxExportBatchSize: defaultMaxExportBatchSize,
		}
		if err := defaults.Set(conf); err != nil {
			return nil, fmt.Errorf("failed to set xatu defaults: %w", err)
		}

		return xatuOutput.New(name, conf, p.log.WithField("sink", name), filterConfig, shippingMethod)

	default:
		return nil, fmt.Errorf("unknown output type: %s", outConfig.Type)
	}
}

// noopPublisher is a no-op implementation for when Xatu is disabled.
type noopPublisher struct{}

// NewNoopPublisher creates a Publisher that does nothing.
func NewNoopPublisher() Publisher {
	return &noopPublisher{}
}

func (p *noopPublisher) Start(_ context.Context) error {
	return nil
}

func (p *noopPublisher) Stop(_ context.Context) error {
	return nil
}

func (p *noopPublisher) Publish(_ context.Context, _ *xatu.DecoratedEvent) error {
	return nil
}

func (p *noopPublisher) ClientMeta() *xatu.ClientMeta {
	return nil
}
