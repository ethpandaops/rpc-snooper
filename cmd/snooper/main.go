package main

import (
	"os"
	"strconv"

	"github.com/ethpandaops/rpc-snooper/snooper"
	"github.com/ethpandaops/rpc-snooper/utils"
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

	rpcSnooper, err := snooper.NewSnooper(cliArgs.target, logger)
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
