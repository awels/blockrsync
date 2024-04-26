package blockrsync

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/dop251/diskrsync"
	"github.com/dop251/spgz"
	"github.com/go-logr/logr"
)

type BlockRsyncOptions struct {
	NoCompress bool
	Verbose    bool
}

type BlockrsyncServer struct {
	targetFile string
	port       int
	opts       *BlockRsyncOptions
	log        logr.Logger
}

func NewBlockrsyncServer(targetFile string, port int, opts *BlockRsyncOptions, logger logr.Logger) *BlockrsyncServer {
	return &BlockrsyncServer{
		targetFile: targetFile,
		port:       port,
		opts:       opts,
		log:        logger,
	}
}

func (b *BlockrsyncServer) StartServer() error {
	f, err := os.OpenFile(b.targetFile, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	file, useReadBuffer, err := b.openFileReader(f)
	defer func() {
		cerr := file.Close()
		if err == nil {
			err = cerr
		}
	}()

	size, err := b.getFileSize(file)
	if err != nil {
		return err
	}

	b.log.Info("Listening for tcp connection", "port", fmt.Sprintf(":%d", b.port))
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", b.port))
	if err != nil {
		return err
	}
	conn, err := listener.Accept()
	if err != nil {
		return err
	}

	err = diskrsync.Target(file, size, conn, conn, useReadBuffer, b.opts.Verbose, nil, nil)
	if err != nil {
		return err
	}
	return nil
}

func (b *BlockrsyncServer) openFileReader(f *os.File) (spgz.SparseFile, bool, error) {
	var w spgz.SparseFile
	useReadBuffer := false

	b.log.Info("Opened file", "file", b.targetFile)
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}

	if info.Mode()&(os.ModeDevice|os.ModeCharDevice) != 0 {
		w = spgz.NewSparseFileWithoutHolePunching(f)
		useReadBuffer = true
	} else if !b.opts.NoCompress {
		sf, err := spgz.NewFromFileSize(f, os.O_RDWR|os.O_CREATE, diskrsync.DefTargetBlockSize)
		if err != nil {
			if !errors.Is(err, spgz.ErrInvalidFormat) {
				if errors.Is(err, spgz.ErrPunchHoleNotSupported) {
					err = fmt.Errorf(
						"target does not support compression. Try with -no-compress option (error was '%w')", err)
				}
				return nil, false, err
			}
		} else {
			w = &diskrsync.FixingSpgzFileWrapper{SpgzFile: sf}
		}
	}

	if w == nil {
		w = spgz.NewSparseFileWithFallback(f)
		useReadBuffer = true
	}
	return w, useReadBuffer, nil
}

func (b *BlockrsyncServer) getFileSize(file spgz.SparseFile) (int64, error) {
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return int64(0), err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return int64(0), err
	}

	b.log.Info("Size", "size", size)
	return size, nil
}
