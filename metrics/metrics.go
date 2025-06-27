package metrics

import (
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricsEntry struct { //nolint:revive // ignore
	Server        string
	Scheme        string
	Method        string
	Hostname      string
	Status        string
	URI           string
	JRPCMethod    string
	BytesSent     int64
	BytesReceived int64
	Duration      float64
}

var (
	tagNames = []string{
		"server",
		"scheme",
		"method",
		"hostname",
		"status",
		"uri",
		"jrpc_method",
	}

	requestCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ngx_request_count",
		Help: "request count",
	}, tagNames)

	requestsSizeCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ngx_request_size_bytes",
		Help: "request size in bytes",
	}, tagNames)

	responseSizeCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ngx_response_size_bytes",
		Help: "response size in bytes",
	}, tagNames)

	requestDurationHistogramVec = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "ngx_request_duration_seconds",
		Help: "request serving time in seconds",
	}, tagNames)
)

func init() {
	prometheus.MustRegister(
		requestCounter,
		requestsSizeCounter,
		responseSizeCounter,
		requestDurationHistogramVec,
	)
}

func PrometheusMetricsRegister(l *MetricsEntry) {
	tags := []string{
		l.Server,
		l.Scheme,
		l.Method,
		l.Hostname,
		l.Status,
		l.URI,
		l.JRPCMethod,
	}

	requestCounter.WithLabelValues(tags...).Inc()
	responseSizeCounter.WithLabelValues(tags...).Add(float64(l.BytesSent))
	requestsSizeCounter.WithLabelValues(tags...).Add(float64(l.BytesReceived))
	requestDurationHistogramVec.WithLabelValues(tags...).Observe(l.Duration)
}

func PrometheusListener(listen string) {
	r := http.NewServeMux()
	r.Handle("/metrics", promhttp.Handler())

	httpServer := &http.Server{
		Addr:              listen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to listen on %s: %s", listen, err)
	}
}

func CreateMetricsEntryFromContexts(target *url.URL, reqCtx *types.RequestContext, respCtx *types.ResponseContext) *MetricsEntry {
	entry := &MetricsEntry{
		Server:     target.Host,
		Scheme:     target.Scheme,
		JRPCMethod: "",
	}

	if reqCtx != nil {
		entry.Method = reqCtx.Method
		entry.Hostname = reqCtx.URL.Host
		entry.URI = reqCtx.URL.Path

		if reqCtx.URL.RawQuery != "" {
			entry.URI += "?" + reqCtx.URL.RawQuery
		}

		entry.BytesReceived = int64(len(reqCtx.BodyBytes))
	}

	if respCtx != nil {
		if jrpcMethod := respCtx.CallCtx.GetData(0, "jrpc_method"); jrpcMethod != nil {
			if jrpcMethodStr, ok := jrpcMethod.(string); ok {
				entry.JRPCMethod = jrpcMethodStr
			}
		}

		entry.Status = strconv.Itoa(respCtx.StatusCode)
		entry.BytesSent = int64(len(respCtx.BodyBytes))
		entry.Duration = respCtx.Duration.Seconds()
	}

	return entry
}
