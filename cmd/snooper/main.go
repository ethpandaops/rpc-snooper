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

	flags.Parse(os.Args)

	if cliArgs.help {
		flags.PrintDefaults()
		return
	}

	logrus.WithFields(logrus.Fields{
		"version": utils.GetBuildVersion(),
	}).Infof("initializing rpc-snooper")
	if cliArgs.version {
		return
	}

	//fmt.Printf("%v", flags.Args())
	if flags.NArg() < 2 || flags.Arg(1) == "" {
		logrus.Error("Target URL missing")
		return
	}
	cliArgs.target = flags.Arg(1)
	logrus.Infof("target url: %v", cliArgs.target)

	logger := logrus.New()
	logger.SetFormatter(&utils.SnooperFormatter{})
	if cliArgs.verbose {
		logger.SetLevel(logrus.DebugLevel)
	}

	rpcSnooper, err := snooper.NewSnooper(cliArgs.target, logger)
	if err != nil {
		logrus.Errorf("Failed initializing server: %v", err)
	}

	err = rpcSnooper.StartServer(cliArgs.bind, cliArgs.port, cliArgs.noapi)
	if err != nil {
		logrus.Errorf("Failed processing server: %v", err)
	}

}
