package xatu

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/sirupsen/logrus"
)

// Service is the main Xatu integration service that coordinates event handlers and publishing.
type Service interface {
	// Start initializes the service and all its components.
	// It first fetches execution metadata, then starts the publisher.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the service.
	Stop(ctx context.Context) error

	// Router returns the event router for module integration.
	Router() *Router

	// Publisher returns the event publisher.
	Publisher() Publisher

	// IsEnabled returns whether the service is enabled.
	IsEnabled() bool

	// ExecutionMetadata returns the current execution client metadata.
	ExecutionMetadata() *ExecutionMetadata

	// UpdateExecutionMetadata updates the cached execution metadata from an observed response.
	UpdateExecutionMetadata(versions []ClientVersionV1)
}

type service struct {
	config          *Config
	log             logrus.FieldLogger
	publisher       Publisher
	router          *Router
	metadataFetcher *ExecutionMetadataFetcher

	mu      sync.RWMutex
	started bool
}

// NewService creates a new Xatu Service instance.
func NewService(config *Config, targetURL *url.URL, log logrus.FieldLogger) (Service, error) {
	if config == nil || !config.Enabled {
		return &noopService{}, nil
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid xatu config: %w", err)
	}

	xatuLog := log.WithField("component", "xatu")
	metadataFetcher := NewExecutionMetadataFetcher(targetURL, config.JWTSecret, xatuLog)
	pub := NewPublisher(config, log)

	// Wire up the metadata provider so ClientMeta includes execution info
	pub.SetMetadataProvider(metadataFetcher)

	s := &service{
		config:          config,
		log:             xatuLog,
		publisher:       pub,
		router:          NewRouter(log),
		metadataFetcher: metadataFetcher,
	}

	// Register event handlers
	s.registerHandlers()

	return s, nil
}

func (s *service) registerHandlers() {
	// Register engine_getBlobs handler
	s.router.Register(NewEngineGetBlobsHandler(s.publisher, s.log))

	// Register engine_newPayload handler
	s.router.Register(NewEngineNewPayloadHandler(s.publisher, s.log))

	// Register engine_getClientVersion handler for passive metadata updates
	s.router.Register(NewEngineClientVersionHandler(s.log, s.metadataFetcher.Update))

	s.log.WithField("handler_count", s.router.HandlerCount()).Info("registered xatu event handlers")
}

// Start initializes the service.
// Metadata fetching runs in the background and doesn't block startup.
func (s *service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	// Start the publisher first so the service is ready to receive events
	if err := s.publisher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start publisher: %w", err)
	}

	// Start metadata fetching in background (non-blocking)
	// This allows the snooper to start accepting connections immediately
	go func() {
		s.log.Info("starting background execution metadata fetch...")

		if err := s.metadataFetcher.Start(ctx); err != nil {
			s.log.WithError(err).Warn("failed to fetch execution metadata (EL may not support engine_getClientVersionV1)")
		} else {
			s.log.Info("execution metadata fetch completed successfully")
		}
	}()

	s.started = true
	s.log.Info("xatu service started")

	return nil
}

// Stop gracefully shuts down the service.
func (s *service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	// Stop metadata fetcher
	s.metadataFetcher.Stop()

	if err := s.publisher.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop publisher: %w", err)
	}

	s.started = false
	s.log.Info("xatu service stopped")

	return nil
}

// Router returns the event router.
func (s *service) Router() *Router {
	return s.router
}

// Publisher returns the event publisher.
func (s *service) Publisher() Publisher {
	return s.publisher
}

// IsEnabled returns whether the service is enabled.
func (s *service) IsEnabled() bool {
	return s.config != nil && s.config.Enabled
}

// ExecutionMetadata returns the current execution client metadata.
func (s *service) ExecutionMetadata() *ExecutionMetadata {
	return s.metadataFetcher.Get()
}

// UpdateExecutionMetadata updates the cached execution metadata from an observed response.
func (s *service) UpdateExecutionMetadata(versions []ClientVersionV1) {
	s.metadataFetcher.Update(versions)
}

// noopService is a no-op implementation for when Xatu is disabled.
type noopService struct{}

func (s *noopService) Start(_ context.Context) error {
	return nil
}

func (s *noopService) Stop(_ context.Context) error {
	return nil
}

func (s *noopService) Router() *Router {
	return nil
}

func (s *noopService) ExecutionMetadata() *ExecutionMetadata {
	return nil
}

func (s *noopService) UpdateExecutionMetadata(_ []ClientVersionV1) {
}

func (s *noopService) Publisher() Publisher {
	return NewNoopPublisher()
}

func (s *noopService) IsEnabled() bool {
	return false
}
