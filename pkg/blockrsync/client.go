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
	sourceFile    string
	targetAddress string
	hasher        Hasher
	port          int
	sourceSize    int64
	opts          *BlockRsyncOptions
	log           logr.Logger
}

func NewBlockrsyncClient(sourceFile, targetAddress string, port int, opts *BlockRsyncOptions, logger logr.Logger) *BlockrsyncClient {
	return &BlockrsyncClient{
		sourceFile:    sourceFile,
		targetAddress: targetAddress,
		hasher:        NewFileHasher(int64(opts.BlockSize), logger.WithName("hasher")),
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
	b.log.Info("Opened file", "file", b.sourceFile)
	defer f.Close()

	size, err := b.hasher.HashFile(b.sourceFile)
	if err != nil {
		return err
	}
	b.sourceSize = size
	b.log.V(5).Info("Hashed file", "filename", b.sourceFile, "size", size)
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

func (b *BlockrsyncClient) writeBlocksToServer(writer io.Writer, offsets []int64, f *os.File, syncProgress *progress) error {
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
	syncProgress.Start(int64(len(offsets)) * b.hasher.BlockSize())
	buf := make([]byte, b.hasher.BlockSize())
	for i, offset := range offsets {
		b.log.V(5).Info("Sending data", "offset", offset, "index", i, "blocksize", b.hasher.BlockSize())
		n, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return err
		}
		if err := binary.Write(writer, binary.LittleEndian, offset); err != nil {
			return err
		}
		if isEmptyBlock(buf) {
			b.log.V(5).Info("Skipping empty block", "offset", offset)
			writer.Write([]byte{Hole})
		} else {
			writer.Write([]byte{Block})
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
		syncProgress.Update(int64(i) * b.hasher.BlockSize())
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
