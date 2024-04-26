package blockrsync

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/dop251/diskrsync"
	"github.com/dop251/spgz"
	"github.com/go-logr/logr"
)

type BlockrsyncClient struct {
	sourceFile    string
	targetAddress string
	port          int
	opts          *BlockRsyncOptions
	log           logr.Logger
}

func NewBlockrsyncClient(sourceFile, targetAddress string, port int, opts *BlockRsyncOptions, logger logr.Logger) *BlockrsyncClient {
	return &BlockrsyncClient{
		sourceFile:    sourceFile,
		targetAddress: targetAddress,
		port:          port,
		opts:          opts,
		log:           logger,
	}
}

func (b *BlockrsyncClient) ConnectToTarget() error {
	f, err := os.Open(b.sourceFile)
	if err != nil {
		return err
	}
	b.log.Info("Opened filed", "file", b.sourceFile)
	defer f.Close()
	var src io.ReadSeeker

	// Try to open as an spgz file
	sf, err := spgz.NewFromFile(f, os.O_RDONLY)
	if err != nil {
		if !errors.Is(err, spgz.ErrInvalidFormat) {
			return err
		}
		b.log.Info("Not an spgz file")
		src = f
	} else {
		b.log.Info("spgz file")
		src = sf
	}

	size, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	_, err = src.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	retry := true
	var conn net.Conn
	retryCount := 0
	for retry {
		conn, err = net.Dial("tcp", fmt.Sprintf("%s:%d", b.targetAddress, b.port))
		retry = err != nil
		if err != nil {
			b.log.Error(err, "Unable to connect to target")
		}
		if retry {
			retryCount++
			time.Sleep(time.Second)
			if retryCount > 30 {
				return fmt.Errorf("unable to connect to target after %d retries", retryCount)
			}
		}
	}
	defer conn.Close()
	b.log.Info("source", "size", size)
	calcProgress := &progress{
		progressType: "calc progress",
		logger:       b.log,
		start:        float64(0),
	}
	syncProgress := &progress{
		progressType: "sync progress",
		logger:       b.log,
		start:        float64(50),
	}
	return diskrsync.Source(src, size, conn, conn, true, b.opts.Verbose, calcProgress, syncProgress)
}
