package xatu

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

// Service is the main Xatu integration service that coordinates event handlers and publishing.
type Service interface {
	// Start initializes the service and all its components.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the service.
	Stop(ctx context.Context) error

	// Router returns the event router for module integration.
	Router() *Router

	// Publisher returns the event publisher.
	Publisher() Publisher

	// IsEnabled returns whether the service is enabled.
	IsEnabled() bool
}

type service struct {
	config    *Config
	log       logrus.FieldLogger
	publisher Publisher
	router    *Router

	mu      sync.RWMutex
	started bool
}

// NewService creates a new Xatu Service instance.
func NewService(config *Config, log logrus.FieldLogger) (Service, error) {
	if config == nil || !config.Enabled {
		return &noopService{}, nil
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid xatu config: %w", err)
	}

	s := &service{
		config:    config,
		log:       log.WithField("component", "xatu"),
		publisher: NewPublisher(config, log),
		router:    NewRouter(log),
	}

	// Register event handlers
	s.registerHandlers()

	return s, nil
}

func (s *service) registerHandlers() {
	// Register engine_getBlobs handler
	s.router.Register(NewEngineGetBlobsHandler(s.publisher, s.log))

	s.log.WithField("handler_count", s.router.HandlerCount()).Info("registered xatu event handlers")
}

// Start initializes the service.
func (s *service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	if err := s.publisher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start publisher: %w", err)
	}

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

func (s *noopService) Publisher() Publisher {
	return NewNoopPublisher()
}

func (s *noopService) IsEnabled() bool {
	return false
}
