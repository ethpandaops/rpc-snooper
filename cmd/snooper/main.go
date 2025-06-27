package main

import (
	"os"

	"github.com/ethpandaops/rpc-snooper/snooper"
	"github.com/ethpandaops/rpc-snooper/utils"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

type CliArgs struct {
	verbose bool
	version bool
	help    bool
	target  string
	bind    string
	port    int
	nocolor bool
	noapi   bool
	apiPort int
	apiAuth string
}

func main() {
	cliArgs := CliArgs{}

	flags := pflag.NewFlagSet("snooper", pflag.ExitOnError)
	flags.BoolVarP(&cliArgs.verbose, "verbose", "v", false, "Run with verbose output")
	flags.BoolVarP(&cliArgs.version, "version", "V", false, "Print version information")
	flags.BoolVarP(&cliArgs.help, "help", "h", false, "Run with verbose output")
	flags.StringVarP(&cliArgs.bind, "bind-address", "b", "127.0.0.1", "Address to bind to and listen for incoming requests.")
	flags.IntVarP(&cliArgs.port, "port", "p", 3000, "Port to listen for incoming requests.")
	flags.BoolVar(&cliArgs.nocolor, "no-color", false, "Do not use terminal colors in output")
	flags.BoolVar(&cliArgs.noapi, "no-api", false, "Do not provide management REST api")
	flags.IntVar(&cliArgs.apiPort, "api-port", 0, "Optional separate port for the snooper API endpoints")
	flags.StringVar(&cliArgs.apiAuth, "api-auth", "", "Optional authentication for API endpoints (format: user:pass,user2:pass2,...)")

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

	if flags.NArg() < 2 || flags.Arg(1) == "" {
		logger.Error("Target URL missing")
		return
	}

	cliArgs.target = flags.Arg(1)
	logger.Infof("target url: %v", cliArgs.target)

	rpcSnooper, err := snooper.NewSnooper(cliArgs.target, logger)
	if err != nil {
		logger.Errorf("Failed initializing server: %v", err)
	}

	// Start separate API server if api-port is specified
	if cliArgs.apiPort > 0 {
		err = rpcSnooper.StartAPIServer(cliArgs.bind, cliArgs.apiPort, cliArgs.apiAuth)
		if err != nil {
			logger.Errorf("Failed starting API server: %v", err)
			return
		}
	}

	err = rpcSnooper.StartServer(cliArgs.bind, cliArgs.port, cliArgs.noapi)
	if err != nil {
		logger.Errorf("Failed processing server: %v", err)
	}
}
