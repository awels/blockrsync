package blockrsync

import (
	"encoding/binary"
	"io"

	"github.com/go-logr/logr"
)

const (
	Hole byte = iota
	Block
)

type BlockReader struct {
	source     io.Reader
	buf        []byte
	offset     int64
	offsetType byte
	log        logr.Logger
}

func NewBlockReader(source io.Reader, blockSize int, log logr.Logger) *BlockReader {
	return &BlockReader{
		source: source,
		buf:    make([]byte, blockSize),
		log:    log,
	}
}

func (b *BlockReader) Next() (bool, error) {
	var offset int64
	if err := binary.Read(b.source, binary.LittleEndian, &offset); err != nil {
		b.log.V(5).Info("Failed to read offset", "error", err)
		return handleReadError(err, nocallback)
	}
	b.offset = offset

	offsetType := make([]byte, 1)
	if n, err := b.source.Read(offsetType); err != nil || n != 1 {
		b.log.V(5).Info("Failed to read offset type", "error", err, "bytes", n)
		return handleReadError(err, nocallback)
	}
	b.offsetType = offsetType[0]
	if !b.IsHole() {
		if n, err := io.ReadFull(b.source, b.buf[:cap(b.buf)]); err != nil {
			b.log.V(5).Info("Failed to read complete block", "error", err, "bytes", n)
			return handleReadError(err, func() {
				b.buf = b.buf[:n]
			})
		}
	}
	return true, nil
}

func (b *BlockReader) Offset() int64 {
	return b.offset
}

func (b *BlockReader) IsHole() bool {
	return b.offsetType == Hole
}

func (b *BlockReader) Block() []byte {
	return b.buf
}

func handleReadError(err error, callback func()) (bool, error) {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		callback()
		return false, nil
	} else {
		return false, err
	}
}

func nocallback() {
	// No call to callback
}
