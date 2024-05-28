package main

import (
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap/zapcore"

	"github.com/spf13/pflag"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/awels/blockrsync/pkg/blockrsync"
)

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage: %s [devicepath] [flags]\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	var (
		sourceMode    = flag.Bool("source", false, "Source mode")
		targetMode    = flag.Bool("target", false, "Target mode")
		targetAddress = flag.String("target-address", "", "address of the server, source only")
		port          = flag.Int("port", 8000, "port to listen on or connect to")
	)
	opts := blockrsync.BlockRsyncOptions{}

	flag.BoolVar(&opts.Preallocation, "preallocate", false, "Preallocate empty file space")
	flag.IntVar(&opts.BlockSize, "block-size", 65536, "block size, must be > 0 and a multiple of 4096")

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

	if opts.BlockSize <= 0 || opts.BlockSize%4096 != 0 {
		fmt.Fprintf(os.Stderr, "block-size must be > 0 and a multiple of 4096\n")
		usage()
	}
	if *sourceMode && !*targetMode {
		if targetAddress == nil || *targetAddress == "" {
			fmt.Fprintf(os.Stderr, "target-address must be specified with source flag\n")
			usage()
			os.Exit(1)
		}
		blockrsyncClient := blockrsync.NewBlockrsyncClient(os.Args[1], *targetAddress, *port, &opts, logger)
		if err := blockrsyncClient.ConnectToTarget(); err != nil {
			logger.Error(err, "Unable to connect to target", "source file", os.Args[1], "target address", *targetAddress)
			// time.Sleep(5 * time.Minute)
			os.Exit(1)
		}
	} else if *targetMode && !*sourceMode {
		blockrsyncServer := blockrsync.NewBlockrsyncServer(os.Args[1], *port, &opts, logger)
		if err := blockrsyncServer.StartServer(); err != nil {
			logger.Error(err, "Unable to start server to write to file", "target file", os.Args[1])
			// time.Sleep(5 * time.Minute)
			os.Exit(1)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Either source or target must be defined\n")
		usage()
		os.Exit(1)
	}
	// time.Sleep(5 * time.Minute)
	logger.Info("Successfully completed sync")
}
