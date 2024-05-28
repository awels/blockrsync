package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/awels/blockrsync/pkg/proxy"
)

type arrayFlags []string

func (i *arrayFlags) String() string {
	return strings.Join(*i, ",")
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	var (
		sourceMode     = flag.Bool("source", false, "Source mode")
		targetMode     = flag.Bool("target", false, "Target mode")
		targetAddress  = flag.String("target-address", "", "address of the server, source only")
		controlFile    = flag.String("control-file", "", "name and path to file to write when finished")
		listenPort     = flag.Int("listen-port", 9080, "port to listen on")
		targetPort     = flag.Int("target-port", 9000, "target port to connect to")
		blockrsyncPath = flag.String("blockrsync-path", "/blockrsync", "path to blockrsync binary")
		blockSize      = flag.Int("block-size", 65536, "block size, must be > 0 and a multiple of 4096")
	)

	var identifiers arrayFlags

	flag.Var(&identifiers, "identifier", "identifier of the file, multiple allowed")

	zapopts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
		DestWriter:  os.Stdout,
	}
	zapopts.BindFlags(flag.CommandLine)

	// Import flags into pflag so they can be bound by viper
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()
	logger := zap.New(zap.UseFlagOptions(&zapopts))

	if controlFile == nil || *controlFile == "" {
		fmt.Fprintf(os.Stderr, "control-file must be specified\n")
		os.Exit(1)
	}
	defer func() {
		logger.Info("Writing control file", "file", *controlFile)
		if err := createControlFile(*controlFile); err != nil {
			logger.Error(err, "Unable to create control file")
		}
	}()

	if *sourceMode && !*targetMode {
		if targetAddress == nil || *targetAddress == "" {
			fmt.Fprintf(os.Stderr, "target-address must be specified with source flag\n")
			os.Exit(1)
		}
		if len(identifiers) > 1 || len(identifiers) == 0 {
			fmt.Fprintf(os.Stderr, "Only one identifier must be specified in source mode\n")
			os.Exit(1)
		}
		client := proxy.NewProxyClient(*listenPort, *targetPort, *targetAddress, logger)

		if err := client.ConnectToTarget(identifiers[0]); err != nil {
			logger.Error(err, "Unable to connect to target", "identifier", identifiers[0], "target address", *targetAddress)
			os.Exit(1)
		}
	} else if *targetMode && !*sourceMode {
		if len(identifiers) == 0 {
			fmt.Fprintf(os.Stderr, "At least one identifier must be specified in target mode\n")
			os.Exit(1)
		}
		server := proxy.NewProxyServer(*blockrsyncPath, *blockSize, *listenPort, identifiers, logger)

		if err := server.StartServer(); err != nil {
			logger.Error(err, "Unable to start server")
			os.Exit(1)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Must specify source or target, but not both\n")
		os.Exit(1)
	}
}

func createControlFile(fileName string) error {
	if err := os.MkdirAll(filepath.Dir(fileName), 0755); err != nil {
		return err
	}
	_, err := os.Create(fileName)
	return err
}
