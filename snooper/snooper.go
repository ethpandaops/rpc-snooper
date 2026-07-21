package snooper

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/rpc-snooper/metrics"
	"github.com/ethpandaops/rpc-snooper/modules"
	"github.com/ethpandaops/rpc-snooper/modules/builtin"
	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/ethpandaops/rpc-snooper/xatu"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"
)

type Snooper struct {
	CallTimeout time.Duration

	target         *url.URL
	logger         logrus.FieldLogger
	api            *API
	moduleManager  *modules.Manager
	apiServer      *http.Server
	apiAuth        map[string]string
	metricsServer  *http.Server
	metricsEnabled bool

	callIndexCounter uint64
	callIndexMutex   sync.Mutex

	orderedProcessor *OrderedProcessor

	// Log truncation
	logTruncationEnabled bool

	// Hide request/response bodies
	hideBodies bool

	// Flow control
	flowEnabled bool
	flowBlocked map[string]bool
	flowMutex   sync.RWMutex

	// Xatu integration
	xatuService     xatu.Service
	metadataFetcher *ExecutionMetadataFetcher
	jwtSecret       string
}

func NewSnooper(target string, logger logrus.FieldLogger, xatuConfig *xatu.Config, jwtSecret string) (*Snooper, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	// Create Xatu service
	xatuService, err := xatu.NewService(xatuConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create xatu service: %w", err)
	}

	snooper := &Snooper{
		CallTimeout: 60 * time.Second,

		target:               targetURL,
		logger:               logger,
		moduleManager:        modules.NewManager(logger),
		logTruncationEnabled: false,
		flowEnabled:          true, // Start with flow enabled by default
		flowBlocked:          make(map[string]bool),
		xatuService:          xatuService,
		jwtSecret:            jwtSecret,
	}

	// Set up metadata fetcher if xatu is enabled
	if xatuService.IsEnabled() {
		snooper.metadataFetcher = NewExecutionMetadataFetcher(targetURL, jwtSecret, logger)

		// Wire up the fetcher as metadata provider for xatu events
		xatuService.SetMetadataProvider(snooper.metadataFetcher)

		// Register callback for passive metadata updates from observed engine_getClientVersion responses
		xatuService.RegisterMetadataUpdateCallback(snooper.metadataFetcher.Update)

		// Register Xatu module
		xatuModule := builtin.NewXatuModule(snooper.moduleManager.GenerateModuleID(), xatuService.Router())

		if err := snooper.moduleManager.RegisterModule(xatuModule, nil); err != nil {
			return nil, fmt.Errorf("failed to register xatu module: %w", err)
		}

		logger.Info("xatu module registered")
	}

	snooper.orderedProcessor = NewOrderedProcessor(snooper)

	return snooper, nil
}

// EnableLogTruncation enables hex truncation in log output.
// Call this once at startup before serving requests.
func (s *Snooper) EnableLogTruncation() {
	s.logTruncationEnabled = true
}

// EnableHideBodies suppresses request/response body logging.
// When enabled, only method, headers, status and timing are logged.
// Call this once at startup before serving requests.
func (s *Snooper) EnableHideBodies() {
	s.hideBodies = true
}

func (s *Snooper) Shutdown() {
	if s.orderedProcessor != nil {
		s.orderedProcessor.Stop()
	}

	if s.metadataFetcher != nil {
		s.metadataFetcher.Stop()
	}

	if s.xatuService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.xatuService.Stop(ctx); err != nil {
			s.logger.WithError(err).Error("failed to stop xatu service")
		}
	}
}

func (s *Snooper) StartServer(host string, port int, noAPI bool, authConfig string) error {
	s.configureAPIAuth(authConfig)

	// Start Xatu service if enabled
	// Note: We use context.Background() because the xatu service workers
	// need a long-lived context that won't be cancelled until shutdown.
	if s.xatuService != nil && s.xatuService.IsEnabled() {
		if err := s.xatuService.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start xatu service: %w", err)
		}

		s.logger.Info("xatu service started")

		// Start metadata fetching in background (non-blocking)
		// This allows the snooper to start accepting connections immediately
		if s.metadataFetcher != nil {
			go func() {
				s.logger.Info("starting background execution metadata fetch...")

				if err := s.metadataFetcher.Start(context.Background()); err != nil {
					s.logger.WithError(err).Warn("failed to fetch execution metadata (EL may not support engine_getClientVersionV1)")
				} else {
					s.logger.Info("execution metadata fetch completed successfully")
				}
			}()
		}
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf("%v:%v", host, port),
		Handler:           s.newRootHandler(noAPI),
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.logger.Infof("listening on: %v", srv.Addr)

	if !noAPI && len(s.apiAuth) > 0 {
		s.logger.Infof("control API authentication enabled for %d users", len(s.apiAuth))
	}

	return srv.ListenAndServe()
}

// newRootHandler builds the proxy-port handler: the control API under
// /_snooper/ (guarded by Basic auth when api-auth is configured) plus the proxy
// itself. Auth is scoped to the control subrouter so proxied traffic is never
// gated.
func (s *Snooper) newRootHandler(noAPI bool) http.Handler {
	router := mux.NewRouter()

	if !noAPI {
		s.api = newAPI(s)
		apiRouter := router.PathPrefix("/_snooper/").Subrouter()

		if len(s.apiAuth) > 0 {
			apiRouter.Use(s.requireAPIAuth)
		}

		s.api.initRouter(apiRouter)
	}

	router.PathPrefix("/").Handler(s)

	n := negroni.New()
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	return n
}

func (s *Snooper) StartAPIServer(host string, port int, authConfig string) error {
	s.configureAPIAuth(authConfig)

	router := mux.NewRouter()

	// Only expose /_snooper endpoints on this API server
	s.api = newAPI(s)
	apiRouter := router.PathPrefix("/_snooper/").Subrouter()
	s.api.initRouter(apiRouter)

	n := negroni.New()
	n.Use(negroni.NewRecovery())

	// Add authentication middleware if auth is configured
	if len(s.apiAuth) > 0 {
		n.UseFunc(s.authMiddleware)
	}

	n.UseHandler(router)

	s.apiServer = &http.Server{
		Addr:              fmt.Sprintf("%v:%v", host, port),
		Handler:           n,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.logger.Infof("API server listening on: %v", s.apiServer.Addr)

	if len(s.apiAuth) > 0 {
		s.logger.Infof("API authentication enabled for %d users", len(s.apiAuth))
	}

	go func() {
		if err := s.apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("API server error: %v", err)
		}
	}()

	return nil
}

func (s *Snooper) StartMetricsServer(host string, port int) error {
	s.metricsEnabled = true

	router := mux.NewRouter()
	router.Handle("/metrics", promhttp.Handler())

	s.metricsServer = &http.Server{
		Addr:              fmt.Sprintf("%v:%v", host, port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.logger.Infof("Metrics server listening on: %v", s.metricsServer.Addr)

	go func() {
		if err := s.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("Metrics server error: %v", err)
		}
	}()

	return nil
}

func (s *Snooper) collectMetrics(req *http.Request, respCtx *types.ResponseContext) {
	// Create request context for metrics collection
	reqCtx := &types.RequestContext{
		Method:    req.Method,
		URL:       req.URL,
		Headers:   req.Header,
		Timestamp: time.Now(),
	}

	// Create metrics entry
	metricsEntry := metrics.CreateMetricsEntryFromContexts(s.target, reqCtx, respCtx)

	// Extract jrpc_method from stored context data
	if ctx, ok := respCtx.CallCtx.(*ProxyCallContext); ok {
		if jrpcMethod := ctx.GetData(0, "jrpc_method"); jrpcMethod != nil {
			if method, ok := jrpcMethod.(string); ok {
				metricsEntry.JRPCMethod = method
			}
		}
	}

	metrics.PrometheusMetricsRegister(metricsEntry)
}

// configureAPIAuth parses the api-auth config (user:pass,user2:pass2,...) into
// the credential map. Safe to call from more than one start path.
func (s *Snooper) configureAPIAuth(authConfig string) {
	if authConfig == "" {
		return
	}

	s.apiAuth = make(map[string]string)

	for _, cred := range strings.Split(authConfig, ",") {
		parts := strings.SplitN(cred, ":", 2)
		if len(parts) == 2 {
			s.apiAuth[parts[0]] = parts[1]
		}
	}
}

// authorized reports whether the request carries valid Basic credentials.
func (s *Snooper) authorized(r *http.Request) bool {
	const prefix = "Basic "

	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return false
	}

	credentials := string(decoded)

	colonIndex := strings.IndexByte(credentials, ':')
	if colonIndex < 0 {
		return false
	}

	username := credentials[:colonIndex]
	password := credentials[colonIndex+1:]

	expectedPassword, ok := s.apiAuth[username]

	return ok && subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) == 1
}

// requireAPIAuth is mux middleware that enforces Basic auth on the routes it
// wraps. It guards the control API mounted on the proxy port.
func (s *Snooper) requireAPIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			s.sendUnauthorized(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware is the negroni form of the same check, used by the separate
// API server.
func (s *Snooper) authMiddleware(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if !s.authorized(r) {
		s.sendUnauthorized(w)
		return
	}

	next(w, r)
}

func (s *Snooper) sendUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Snooper API"`)
	w.WriteHeader(http.StatusUnauthorized)
	w.Header().Set("Content-Type", "application/json")

	j := json.NewEncoder(w)

	err := j.Encode(map[string]any{
		"status":  "error",
		"message": "Unauthorized",
	})
	if err != nil {
		s.logger.Errorf("failed writing unauthorized response: %v", err)
	}
}

func (s *Snooper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := s.processProxyCall(w, r)
	if err != nil {
		s.logger.Errorf("call failed: %v", err)

		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		j := json.NewEncoder(w)

		err = j.Encode(map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
		if err != nil {
			s.logger.Errorf("failed writing response: %v", err)
		}
	}
}
