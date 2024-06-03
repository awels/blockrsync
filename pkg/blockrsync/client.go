package blockrsync

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang/snappy"
)

type BlockrsyncClient struct {
	sourceFile         string
	hasher             Hasher
	sourceSize         int64
	opts               *BlockRsyncOptions
	log                logr.Logger
	connectionProvider ConnectionProvider
}

func NewBlockrsyncClient(sourceFile, targetAddress string, port int, opts *BlockRsyncOptions, logger logr.Logger) *BlockrsyncClient {
	return &BlockrsyncClient{
		sourceFile: sourceFile,
		hasher:     NewFileHasher(int64(opts.BlockSize), logger.WithName("hasher")),
		opts:       opts,
		log:        logger,
		connectionProvider: &NetworkConnectionProvider{
			targetAddress: targetAddress,
			port:          port,
		},
	}
}

func (b *BlockrsyncClient) ConnectToTarget() error {
	f, err := os.Open(b.sourceFile)
	if err != nil {
		return err
	}
	b.log.Info("Opened file", "file", b.sourceFile)
	defer f.Close()

	size, err := b.hasher.HashFile(b.sourceFile)
	if err != nil {
		return err
	}
	b.sourceSize = size
	b.log.V(5).Info("Hashed file", "filename", b.sourceFile, "size", size)
	conn, err := b.connectionProvider.Connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	reader := snappy.NewReader(conn)
	var diff []int64
	if blockSize, sourceHashes, err := b.hasher.DeserializeHashes(reader); err != nil {
		return err
	} else {
		diff, err = b.hasher.DiffHashes(blockSize, sourceHashes)
		if err != nil {
			return err
		}
		if len(diff) == 0 {
			b.log.Info("No differences found")
			return nil
		} else {
			b.log.Info("Differences found", "count", len(diff))
		}
	}
	writer := snappy.NewBufferedWriter(conn)
	defer writer.Close()

	syncProgress := &progress{
		progressType: "sync progress",
		logger:       b.log,
		start:        float64(50),
	}
	if err := b.writeBlocksToServer(writer, diff, f, syncProgress); err != nil {
		return err
	}

	return nil
}

func (b *BlockrsyncClient) writeBlocksToServer(writer io.Writer, offsets []int64, f io.ReaderAt, syncProgress Progress) error {
	b.log.V(3).Info("Writing blocks to server")
	t := time.Now()
	defer func() {
		b.log.V(3).Info("Writing blocks took", "milliseconds", time.Since(t).Milliseconds())
	}()

	b.log.V(5).Info("Sending size of source file")
	if err := binary.Write(writer, binary.LittleEndian, b.sourceSize); err != nil {
		return err
	}
	b.log.V(5).Info("Sorting offsets")
	// Sort diff
	slices.SortFunc(offsets, int64SortFunc)
	b.log.V(5).Info("offsets", "values", offsets)
	if syncProgress != nil {
		syncProgress.Start(int64(len(offsets)) * b.hasher.BlockSize())
	}
	buf := make([]byte, b.hasher.BlockSize())
	for i, offset := range offsets {
		b.log.V(5).Info("Sending data", "offset", offset, "index", i, "blocksize", b.hasher.BlockSize())
		if err := binary.Write(writer, binary.LittleEndian, offset); err != nil {
			return err
		}
		n, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return err
		}
		if isEmptyBlock(buf) {
			b.log.V(5).Info("Skipping empty block", "offset", offset)
			if _, err := writer.Write([]byte{Hole}); err != nil {
				return err
			}
		} else {
			_, err := writer.Write([]byte{Block})
			if err != nil {
				return err
			}
			if int64(n) != b.hasher.BlockSize() {
				b.log.V(5).Info("read last bytes", "count", n)
			}
			buf = buf[:n]
			b.log.V(5).Info("Writing bytes", "count", len(buf))
			_, err = writer.Write(buf)
			if err != nil {
				return err
			}
		}
		if syncProgress != nil {
			syncProgress.Update(int64(i) * b.hasher.BlockSize())
		}
	}
	return nil
}

func isEmptyBlock(buf []byte) bool {
	for _, b := range buf {
		if b != 0 {
			return false
		}
	}
	return true
}

func int64SortFunc(i, j int64) int {
	if j > i {
		return -1
	} else if j < i {
		return 1
	}
	return 0
}

type ConnectionProvider interface {
	Connect() (io.ReadWriteCloser, error)
}

type NetworkConnectionProvider struct {
	targetAddress string
	port          int
}

func (n *NetworkConnectionProvider) Connect() (io.ReadWriteCloser, error) {
	retryCount := 0
	var conn io.ReadWriteCloser
	var err error
	for conn == nil {
		conn, err = net.Dial("tcp", fmt.Sprintf("%s:%d", n.targetAddress, n.port))
		if err != nil {
			if retryCount > 30 {
				return nil, fmt.Errorf("unable to connect to target after %d retries", retryCount)
			}
			time.Sleep(time.Second)
			retryCount++
		}
	}
	return conn, nil
}
