package blockrsync

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/go-logr/logr"
	"github.com/golang/snappy"
)

type BlockRsyncOptions struct {
	Preallocation bool
	BlockSize     int
}

type BlockrsyncServer struct {
	targetFile     string
	targetFileSize int64
	port           int
	hasher         Hasher
	opts           *BlockRsyncOptions
	log            logr.Logger
}

func NewBlockrsyncServer(targetFile string, port int, opts *BlockRsyncOptions, logger logr.Logger) *BlockrsyncServer {
	return &BlockrsyncServer{
		targetFile: targetFile,
		port:       port,
		opts:       opts,
		log:        logger,
		hasher:     NewFileHasher(int64(opts.BlockSize), logger.WithName("hasher")),
	}
}

func (b *BlockrsyncServer) StartServer() error {
	f, err := os.OpenFile(b.targetFile, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	readyChan := make(chan struct{})

	go func() {
		defer func() { readyChan <- struct{}{} }()
		size, err := b.hasher.HashFile(b.targetFile)
		if err != nil {
			b.log.Error(err, "Failed to hash file")
			return
		}
		b.targetFileSize = size
		b.log.Info("Hashed file with size", "filename", b.targetFile, "size", b.targetFileSize)
	}()

	b.log.Info("Listening for tcp connection", "port", fmt.Sprintf(":%d", b.port))
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", b.port))
	if err != nil {
		return err
	}
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	writer := snappy.NewBufferedWriter(conn)
	<-readyChan

	if err := b.writeHashes(writer); err != nil {
		return err
	}
	b.log.Info("Wrote hashes to client, starting diff reader")
	reader := bufio.NewReader(snappy.NewReader(conn))
	if err := b.writeBlocksToFile(f, reader); err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

func (b *BlockrsyncServer) writeHashes(writer io.WriteCloser) error {
	defer writer.Close()
	if err := b.hasher.SerializeHashes(writer); err != nil {
		return err
	}
	b.log.Info("Wrote hashes to client")
	return nil
}

func (b *BlockrsyncServer) writeBlocksToFile(f *os.File, reader io.Reader) error {
	// Read the size of the source file
	var sourceSize int64
	if err := binary.Read(reader, binary.LittleEndian, &sourceSize); err != nil {
		_, err = handleReadError(err, nocallback)
		return err
	}
	if err := b.truncateFileIfNeeded(f, sourceSize, b.targetFileSize); err != nil {
		_, err = handleReadError(err, nocallback)
		return err
	}

	blockReader := NewBlockReader(reader, int(b.hasher.BlockSize()), b.log.WithName("block-reader"))
	cont := true
	var err error
	for cont {
		cont, err = blockReader.Next()
		if err != nil {
			// Ignore error
			return nil
		}
		if blockReader.IsHole() {
			if err := b.handleEmptyBlock(blockReader.Offset(), f); err != nil {
				return err
			}
		} else {
			if err := b.writeBlockToOffset(blockReader.Block(), blockReader.Offset(), f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *BlockrsyncServer) truncateFileIfNeeded(f *os.File, sourceSize, targetSize int64) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if targetSize > sourceSize {
		b.log.V(5).Info("Source size", "size", sourceSize)
		if info.Mode()&(os.ModeDevice|os.ModeCharDevice) == 0 {
			// Not a block device, truncate the file if it is larger than the source file
			// Truncate the target file if it is larger than the source file
			b.log.V(5).Info("Source is smaller than target, truncating file")
			if err := f.Truncate(sourceSize); err != nil {
				return err
			}
		} else {
			// empty out existing blocks
			PunchHole(f, sourceSize, targetSize-sourceSize)
		}
	}
	return nil
}

func (b *BlockrsyncServer) handleEmptyBlock(offset int64, f *os.File) error {
	b.log.V(5).Info("Skipping hole", "offset", offset)
	emptySize := min(b.targetFileSize-offset, b.hasher.BlockSize())
	if b.opts.Preallocation {
		b.log.V(5).Info("Preallocating hole", "offset", offset)
		preallocBuffer := make([]byte, emptySize)
		if n, err := f.WriteAt(preallocBuffer, offset); err != nil || int64(n) != emptySize {
			return err
		}
	} else {
		b.log.V(5).Info("Punching hole", "offset", offset, "size", b.hasher.BlockSize())
		PunchHole(f, offset, b.hasher.BlockSize())
	}
	return nil
}

func (b *BlockrsyncServer) writeBlockToOffset(block []byte, offset int64, ws io.WriteSeeker) error {
	_, err := ws.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}
	if n, err := ws.Write(block); err != nil {
		return err
	} else {
		b.log.V(5).Info("Wrote", "bytes", n)
	}
	return nil
}
