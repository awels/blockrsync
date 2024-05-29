package blockrsync

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/crypto/blake2b"
)

const (
	DefaultBlockSize   = int64(64 * 1024)
	defaultConcurrency = 25
)

type Hasher interface {
	HashFile(file string) (int64, error)
	GetHashes() map[int64][]byte
	DiffHashes(int64, map[int64][]byte) ([]int64, error)
	SerializeHashes(io.Writer) error
	DeserializeHashes(io.Reader) (int64, map[int64][]byte, error)
	BlockSize() int64
}

type OffsetHash struct {
	Offset int64
	Hash   []byte
}

type FileHasher struct {
	hashes    map[int64][]byte
	queue     chan int64
	res       chan OffsetHash
	blockSize int64
	fileSize  int64
	log       logr.Logger
}

func NewFileHasher(blockSize int64, log logr.Logger) Hasher {
	return &FileHasher{
		blockSize: blockSize,
		queue:     make(chan int64, defaultConcurrency),
		res:       make(chan OffsetHash, defaultConcurrency),
		hashes:    make(map[int64][]byte),
		log:       log,
	}
}

func (f *FileHasher) HashFile(fileName string) (int64, error) {
	f.log.V(3).Info("Hashing file", "file", fileName)
	t := time.Now()
	defer func() {
		f.log.V(3).Info("Hashing took", "milliseconds", time.Since(t).Milliseconds())
	}()
	done := make(chan struct{})
	size, err := f.getFileSize(fileName)
	if err != nil {
		return 0, err
	}
	f.fileSize = size
	go f.calculateOffsets(f.fileSize)

	count := f.concurrentHashCount(f.fileSize)
	wg := sync.WaitGroup{}
	go func() {
		wg.Wait()
		done <- struct{}{}
	}()

	for i := 0; i < count; i++ {
		wg.Add(1)
		h, err := blake2b.New512(nil)
		if err != nil {
			return 0, err
		}
		go func(h hash.Hash) {
			defer wg.Done()
			osFile, err := os.Open(fileName)
			if err != nil {
				f.log.Info("Failed to open file", "error", err)
				return
			}
			for offset := range f.queue {
				h.Reset()
				defer osFile.Close()
				if err := f.calculateHash(offset, osFile, h); err != nil {
					f.log.Info("Failed to calculate hash", "offset", offset, "error", err)
					return
				}
			}
		}(h)
	}
	for {
		select {
		case offsetHash := <-f.res:
			f.hashes[offsetHash.Offset] = offsetHash.Hash
		case <-done:
			return f.fileSize, nil
		}
	}
}

func (f *FileHasher) getFileSize(fileName string) (int64, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return int64(0), err
	}
	defer file.Close()
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return int64(0), err
	}
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return int64(0), err
	}
	f.log.V(5).Info("Size", "bytes", size)
	return size, nil
}

func (f *FileHasher) concurrentHashCount(fileSize int64) int {
	return int(math.Min(float64(defaultConcurrency), float64(int(fileSize/f.blockSize))))
}

func (f *FileHasher) calculateOffsets(size int64) {
	var i int64
	defer close(f.queue)
	f.log.V(5).Info("blocksize", "size", f.blockSize)
	for i = 0; i < size; i += f.blockSize {
		f.queue <- i
	}
}

func (f *FileHasher) calculateHash(offset int64, rs io.ReadSeeker, h hash.Hash) error {
	_, err := rs.Seek(int64(offset), 0)
	if err != nil {
		f.log.V(5).Info("Failed to seek")
		return err
	}
	buf := make([]byte, f.blockSize)
	n, err := rs.Read(buf)
	if err != nil {
		f.log.V(5).Info("Failed to read")
		return err
	}
	n, err = h.Write(buf[:n])
	if err != nil {
		f.log.V(5).Info("Failed to write to hash")
		return err
	}
	if n != len(buf) {
		f.log.V(5).Info("Finished reading file")
	}
	offsetHash := OffsetHash{
		Offset: offset,
		Hash:   h.Sum(nil),
	}
	f.res <- offsetHash
	return nil
}

func (f *FileHasher) GetHashes() map[int64][]byte {
	return f.hashes
}

func (f *FileHasher) DiffHashes(blockSize int64, cmpHash map[int64][]byte) ([]int64, error) {
	if blockSize != f.blockSize {
		return nil, errors.New("block size mismatch")
	}
	var diff []int64
	f.log.V(5).Info("Size of hashes ", "hash", len(f.hashes), "incoming hash", len(cmpHash))
	for k, v := range f.hashes {
		if _, ok := cmpHash[k]; !ok {
			// Hash not found in cmpHash
			diff = append(diff, k)
		} else {
			if !bytes.Equal(v, cmpHash[k]) {
				// Hashes don't match
				diff = append(diff, k)
			}
			delete(cmpHash, k)
		}
	}
	for k := range cmpHash {
		// remaining hashes in cmpHash, if the offset is < size of source file
		if k < f.fileSize {
			diff = append(diff, k)
		}
	}
	return diff, nil
}

func (f *FileHasher) SerializeHashes(w io.Writer) error {
	f.log.V(3).Info("Serializing hashes")
	t := time.Now()
	defer func() {
		f.log.V(3).Info("Serializing took", "milliseconds", time.Since(t).Milliseconds())
	}()

	if err := binary.Write(w, binary.LittleEndian, int64(f.blockSize)); err != nil {
		return err
	}
	length := len(f.hashes)
	f.log.V(5).Info("Number of blocks", "size", length)
	if err := binary.Write(w, binary.LittleEndian, int64(length)); err != nil {
		return err
	}
	keys := make([]int64, 0, len(f.hashes))
	for k := range f.hashes {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, int64SortFunc)
	for _, k := range keys {
		f.log.V(5).Info("Writing offset", "offset", k)
		if err := binary.Write(w, binary.LittleEndian, k); err != nil {
			return err
		}
		if len(f.hashes[k]) != 64 {
			return errors.New("invalid hash length")
		}
		if n, err := w.Write(f.hashes[k]); err != nil {
			return err
		} else {
			f.log.V(5).Info("Wrote hash", "bytes", n)
		}
	}
	f.log.V(5).Info("Finished writing hashes")
	return nil
}

func (f *FileHasher) DeserializeHashes(r io.Reader) (int64, map[int64][]byte, error) {
	f.log.V(3).Info("Deserializing hashes")
	t := time.Now()
	defer func() {
		f.log.V(3).Info("Deserializing took", "milliseconds", time.Since(t).Milliseconds())
	}()
	var blockSize int64
	if err := binary.Read(r, binary.LittleEndian, &blockSize); err != nil {
		return 0, nil, err
	}
	var length int64
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return 0, nil, err
	}
	f.log.V(3).Info("Number of blocks to receive", "size", length)
	hashes := make(map[int64][]byte)
	for i := int64(0); i < length; i++ {
		var offset int64
		if err := binary.Read(r, binary.LittleEndian, &offset); err != nil {
			return 0, nil, err
		}
		f.log.V(5).Info("Reading offset", "offset", offset)
		if offset < 0 || offset > length*blockSize {
			return 0, nil, fmt.Errorf("invalid offset %d", offset)
		}
		hash := make([]byte, 64)
		if n, err := io.ReadFull(r, hash); err != nil {
			return 0, nil, err
		} else {
			f.log.V(5).Info("Read hash", "bytes", n, "hash", base64.StdEncoding.EncodeToString(hash))
		}
		hashes[offset] = hash
	}
	f.log.V(3).Info("Number of blocks actually received", "size", len(hashes))
	return blockSize, hashes, nil
}

func (f *FileHasher) BlockSize() int64 {
	return f.blockSize
}
