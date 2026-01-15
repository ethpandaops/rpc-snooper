package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/rpc-snooper/snooper"
	"github.com/ethpandaops/rpc-snooper/utils"
	"github.com/ethpandaops/rpc-snooper/xatu"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

type CliArgs struct {
	verbose     bool
	version     bool
	help        bool
	target      string
	bind        string
	port        int
	nocolor     bool
	noapi       bool
	apiPort     int
	apiBind     string
	apiAuth     string
	metricsPort int
	metricsBind string

	// Xatu integration
	xatuEnabled            bool
	xatuName               string
	xatuOutputs            []string
	xatuLabels             []string
	xatuTLS                bool
	xatuHeaders            []string
	xatuMaxQueueSize       int
	xatuMaxExportBatchSize int
	xatuWorkers            int
	xatuBatchTimeout       time.Duration
	xatuExportTimeout      time.Duration
	xatuKeepAliveEnabled   bool
	xatuKeepAliveTime      time.Duration
	xatuKeepAliveTimeout   time.Duration
}

func getEnvBool(key string, defaultValue bool) bool { //nolint:unparam // ignore
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}

	return defaultValue
}

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}

	return defaultValue
}

func getEnvStringSlice(key string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}

	return nil
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration { //nolint:unparam // ignore
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}

	return defaultValue
}

func buildXatuConfig(args *CliArgs, logger logrus.FieldLogger) *xatu.Config {
	if !args.xatuEnabled {
		return &xatu.Config{Enabled: false}
	}

	config := &xatu.Config{
		Enabled:            true,
		Name:               args.xatuName,
		TLS:                args.xatuTLS,
		Labels:             make(map[string]string),
		Headers:            make(map[string]string),
		Outputs:            make([]xatu.OutputConfig, 0, len(args.xatuOutputs)),
		MaxQueueSize:       args.xatuMaxQueueSize,
		MaxExportBatchSize: args.xatuMaxExportBatchSize,
		Workers:            args.xatuWorkers,
		BatchTimeout:       args.xatuBatchTimeout,
		ExportTimeout:      args.xatuExportTimeout,
		KeepAlive: xatu.KeepAliveConfig{
			Enabled: args.xatuKeepAliveEnabled,
			Time:    args.xatuKeepAliveTime,
			Timeout: args.xatuKeepAliveTimeout,
		},
	}

	// Parse outputs
	for _, out := range args.xatuOutputs {
		outConfig, err := xatu.ParseOutputFlag(out)
		if err != nil {
			logger.WithError(err).Warnf("invalid xatu output: %s", out)

			continue
		}

		config.Outputs = append(config.Outputs, outConfig)
	}

	// Parse labels
	for _, label := range args.xatuLabels {
		key, value, err := xatu.ParseLabelFlag(label)
		if err != nil {
			logger.WithError(err).Warnf("invalid xatu label: %s", label)

			continue
		}

		config.Labels[key] = value
	}

	// Parse headers
	for _, header := range args.xatuHeaders {
		name, value, err := xatu.ParseHeaderFlag(header)
		if err != nil {
			logger.WithError(err).Warnf("invalid xatu header: %s", header)

			continue
		}

		config.Headers[name] = value
	}

	return config
}

func main() {
	// Load defaults from environment variables
	cliArgs := CliArgs{
		verbose:     getEnvBool("SNOOPER_VERBOSE", false),
		version:     getEnvBool("SNOOPER_VERSION", false),
		help:        getEnvBool("SNOOPER_HELP", false),
		bind:        getEnvString("SNOOPER_BIND_ADDRESS", "127.0.0.1"),
		port:        getEnvInt("SNOOPER_PORT", 3000),
		nocolor:     getEnvBool("SNOOPER_NO_COLOR", false),
		noapi:       getEnvBool("SNOOPER_NO_API", false),
		apiPort:     getEnvInt("SNOOPER_API_PORT", 0),
		apiBind:     getEnvString("SNOOPER_API_BIND", "0.0.0.0"),
		apiAuth:     getEnvString("SNOOPER_API_AUTH", ""),
		metricsPort: getEnvInt("SNOOPER_METRICS_PORT", 0),
		metricsBind: getEnvString("SNOOPER_METRICS_BIND", "127.0.0.1"),

		// Xatu defaults from environment
		xatuEnabled:            getEnvBool("SNOOPER_XATU_ENABLED", false),
		xatuName:               getEnvString("SNOOPER_XATU_NAME", ""),
		xatuOutputs:            getEnvStringSlice("SNOOPER_XATU_OUTPUTS"),
		xatuLabels:             getEnvStringSlice("SNOOPER_XATU_LABELS"),
		xatuTLS:                getEnvBool("SNOOPER_XATU_TLS", false),
		xatuHeaders:            getEnvStringSlice("SNOOPER_XATU_HEADERS"),
		xatuMaxQueueSize:       getEnvInt("SNOOPER_XATU_MAX_QUEUE_SIZE", 0),
		xatuMaxExportBatchSize: getEnvInt("SNOOPER_XATU_MAX_EXPORT_BATCH_SIZE", 0),
		xatuWorkers:            getEnvInt("SNOOPER_XATU_WORKERS", 0),
		xatuBatchTimeout:       getEnvDuration("SNOOPER_XATU_BATCH_TIMEOUT", 0),
		xatuExportTimeout:      getEnvDuration("SNOOPER_XATU_EXPORT_TIMEOUT", 0),
		xatuKeepAliveEnabled:   getEnvBool("SNOOPER_XATU_KEEPALIVE_ENABLED", false),
		xatuKeepAliveTime:      getEnvDuration("SNOOPER_XATU_KEEPALIVE_TIME", 0),
		xatuKeepAliveTimeout:   getEnvDuration("SNOOPER_XATU_KEEPALIVE_TIMEOUT", 0),
	}

	flags := pflag.NewFlagSet("snooper", pflag.ExitOnError)
	flags.BoolVarP(&cliArgs.verbose, "verbose", "v", cliArgs.verbose, "Run with verbose output (env: SNOOPER_VERBOSE)")
	flags.BoolVarP(&cliArgs.version, "version", "V", cliArgs.version, "Print version information (env: SNOOPER_VERSION)")
	flags.BoolVarP(&cliArgs.help, "help", "h", cliArgs.help, "Run with verbose output (env: SNOOPER_HELP)")
	flags.StringVarP(&cliArgs.bind, "bind-address", "b", cliArgs.bind, "Address to bind to and listen for incoming requests (env: SNOOPER_BIND_ADDRESS)")
	flags.IntVarP(&cliArgs.port, "port", "p", cliArgs.port, "Port to listen for incoming requests (env: SNOOPER_PORT)")
	flags.BoolVar(&cliArgs.nocolor, "no-color", cliArgs.nocolor, "Do not use terminal colors in output (env: SNOOPER_NO_COLOR)")
	flags.BoolVar(&cliArgs.noapi, "no-api", cliArgs.noapi, "Do not provide management REST api (env: SNOOPER_NO_API)")
	flags.IntVar(&cliArgs.apiPort, "api-port", cliArgs.apiPort, "Optional separate port for the snooper API endpoints (env: SNOOPER_API_PORT)")
	flags.StringVar(&cliArgs.apiBind, "api-bind", cliArgs.apiBind, "Optional address to bind to for the snooper API endpoints (env: SNOOPER_API_BIND)")
	flags.StringVar(&cliArgs.apiAuth, "api-auth", cliArgs.apiAuth, "Optional authentication for API endpoints (format: user:pass,user2:pass2,...) (env: SNOOPER_API_AUTH)")
	flags.IntVar(&cliArgs.metricsPort, "metrics-port", cliArgs.metricsPort, "Optional port for Prometheus metrics endpoint (env: SNOOPER_METRICS_PORT)")
	flags.StringVar(&cliArgs.metricsBind, "metrics-bind", cliArgs.metricsBind, "Optional address to bind to for the Prometheus metrics endpoint (env: SNOOPER_METRICS_BIND)")

	// Xatu flags
	flags.BoolVar(&cliArgs.xatuEnabled, "xatu-enabled", cliArgs.xatuEnabled, "Enable Xatu event publishing (env: SNOOPER_XATU_ENABLED)")
	flags.StringVar(&cliArgs.xatuName, "xatu-name", cliArgs.xatuName, "Instance name for Xatu events (env: SNOOPER_XATU_NAME)")
	flags.StringSliceVar(&cliArgs.xatuOutputs, "xatu-output", cliArgs.xatuOutputs, "Xatu output sink (format: type:address, can be repeated) (env: SNOOPER_XATU_OUTPUTS)")
	flags.StringSliceVar(&cliArgs.xatuLabels, "xatu-label", cliArgs.xatuLabels, "Xatu label (format: key=value, can be repeated) (env: SNOOPER_XATU_LABELS)")
	flags.BoolVar(&cliArgs.xatuTLS, "xatu-tls", cliArgs.xatuTLS, "Enable TLS for xatu:// outputs (env: SNOOPER_XATU_TLS)")
	flags.StringSliceVar(&cliArgs.xatuHeaders, "xatu-header", cliArgs.xatuHeaders, "Xatu output header (format: name=value, can be repeated) (env: SNOOPER_XATU_HEADERS)")
	flags.IntVar(&cliArgs.xatuMaxQueueSize, "xatu-max-queue-size", cliArgs.xatuMaxQueueSize, "Max events to buffer before dropping (env: SNOOPER_XATU_MAX_QUEUE_SIZE)")
	flags.IntVar(&cliArgs.xatuMaxExportBatchSize, "xatu-max-export-batch-size", cliArgs.xatuMaxExportBatchSize, "Max events per batch export (env: SNOOPER_XATU_MAX_EXPORT_BATCH_SIZE)")
	flags.IntVar(&cliArgs.xatuWorkers, "xatu-workers", cliArgs.xatuWorkers, "Number of concurrent export workers (env: SNOOPER_XATU_WORKERS)")
	flags.DurationVar(&cliArgs.xatuBatchTimeout, "xatu-batch-timeout", cliArgs.xatuBatchTimeout, "Time to wait before exporting partial batch (env: SNOOPER_XATU_BATCH_TIMEOUT)")
	flags.DurationVar(&cliArgs.xatuExportTimeout, "xatu-export-timeout", cliArgs.xatuExportTimeout, "Timeout for each export operation (env: SNOOPER_XATU_EXPORT_TIMEOUT)")
	flags.BoolVar(&cliArgs.xatuKeepAliveEnabled, "xatu-keepalive-enabled", cliArgs.xatuKeepAliveEnabled, "Enable gRPC keepalive (env: SNOOPER_XATU_KEEPALIVE_ENABLED)")
	flags.DurationVar(&cliArgs.xatuKeepAliveTime, "xatu-keepalive-time", cliArgs.xatuKeepAliveTime, "Duration after which keepalive ping is sent (env: SNOOPER_XATU_KEEPALIVE_TIME)")
	flags.DurationVar(&cliArgs.xatuKeepAliveTimeout, "xatu-keepalive-timeout", cliArgs.xatuKeepAliveTimeout, "Duration to wait for keepalive response (env: SNOOPER_XATU_KEEPALIVE_TIMEOUT)")

	//nolint:errcheck // ignore
	flags.Parse(os.Args)

	if cliArgs.help {
		flags.PrintDefaults()
		return
	}

	logger := logrus.New()
	formatter := &utils.SnooperFormatter{}
	formatter.Formatter.FullTimestamp = true

	if cliArgs.nocolor {
		formatter.DisableColors()
	} else {
		formatter.EnableColors()
		formatter.Formatter.ForceColors = true
	}

	logger.SetFormatter(formatter)

	if cliArgs.verbose {
		logger.SetLevel(logrus.DebugLevel)
	}

	logger.WithFields(logrus.Fields{
		"version": utils.GetBuildVersion(),
	}).Infof("initializing rpc-snooper")

	if cliArgs.version {
		return
	}

	// Get target URL from command line argument or environment variable
	if flags.NArg() >= 2 && flags.Arg(1) != "" {
		cliArgs.target = flags.Arg(1)
	} else if target := os.Getenv("SNOOPER_TARGET"); target != "" {
		cliArgs.target = target
	} else {
		logger.Error("Target URL missing (provide as argument or set SNOOPER_TARGET env var)")
		return
	}

	logger.Infof("target url: %v", cliArgs.target)

	// Build Xatu config from CLI args
	xatuConfig := buildXatuConfig(&cliArgs, logger)

	rpcSnooper, err := snooper.NewSnooper(cliArgs.target, logger, xatuConfig)
	if err != nil {
		logger.Errorf("Failed initializing server: %v", err)
	}

	// Start separate API server if api-port is specified
	if cliArgs.apiPort > 0 {
		err = rpcSnooper.StartAPIServer(cliArgs.apiBind, cliArgs.apiPort, cliArgs.apiAuth)
		if err != nil {
			logger.Errorf("Failed starting API server: %v", err)
			return
		}
	}

	// Start metrics server if metrics-port is specified
	if cliArgs.metricsPort > 0 {
		err = rpcSnooper.StartMetricsServer(cliArgs.metricsBind, cliArgs.metricsPort)
		if err != nil {
			logger.Errorf("Failed starting metrics server: %v", err)
			return
		}
	}

	err = rpcSnooper.StartServer(cliArgs.bind, cliArgs.port, cliArgs.noapi)
	if err != nil {
		logger.Errorf("Failed processing server: %v", err)
	}
}
