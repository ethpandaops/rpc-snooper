package snooper

import (
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
	"github.com/ethpandaops/rpc-snooper/types"
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
}

func NewSnooper(target string, logger logrus.FieldLogger) (*Snooper, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	return &Snooper{
		CallTimeout: 60 * time.Second,

		target:        targetURL,
		logger:        logger,
		moduleManager: modules.NewManager(logger),
	}, nil
}

func (s *Snooper) StartServer(host string, port int, noAPI bool) error {
	router := mux.NewRouter()

	if !noAPI {
		s.api = newAPI(s)
		apiRouter := router.PathPrefix("/_snooper/").Subrouter()
		s.api.initRouter(apiRouter)
	}

	router.PathPrefix("/").Handler(s)

	n := negroni.New()
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	srv := &http.Server{
		Addr:              fmt.Sprintf("%v:%v", host, port),
		Handler:           n,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.logger.Infof("listening on: %v", srv.Addr)

	return srv.ListenAndServe()
}

func (s *Snooper) StartAPIServer(host string, port int, authConfig string) error {
	// Parse authentication configuration
	if authConfig != "" {
		s.apiAuth = make(map[string]string)

		for _, cred := range strings.Split(authConfig, ",") {
			parts := strings.SplitN(cred, ":", 2)
			if len(parts) == 2 {
				s.apiAuth[parts[0]] = parts[1]
			}
		}
	}

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

func (s *Snooper) authMiddleware(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	// Extract basic auth credentials
	auth := r.Header.Get("Authorization")
	if auth == "" {
		s.sendUnauthorized(w)
		return
	}

	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		s.sendUnauthorized(w)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		s.sendUnauthorized(w)
		return
	}

	credentials := string(decoded)

	colonIndex := strings.IndexByte(credentials, ':')
	if colonIndex < 0 {
		s.sendUnauthorized(w)
		return
	}

	username := credentials[:colonIndex]
	password := credentials[colonIndex+1:]

	// Check credentials
	expectedPassword, ok := s.apiAuth[username]
	if !ok || subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) != 1 {
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
