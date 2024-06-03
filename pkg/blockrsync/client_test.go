package blockrsync

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testFileNameEmpty = "empty.raw"
	testMD5           = "ba3cd24377dde5dfdd58728894004abb"
)

var _ = Describe("blockrsync client tests", func() {
	var (
		client *BlockrsyncClient
		buf    *bytes.Buffer
		file   *bytes.Reader
	)
	BeforeEach(func() {
		opts := BlockRsyncOptions{
			BlockSize:     2,
			Preallocation: false,
		}
		client = NewBlockrsyncClient(filepath.Join(testImagePath, testFileName), "localhost", 8080, &opts, GinkgoLogr.WithName("client"))
		client.sourceSize = 40
		buf = bytes.NewBuffer([]byte{})
		file = bytes.NewReader([]byte{1, 2, 0, 0, 3, 4})
	})

	It("writeBlocksToServer should write a hole to the writer", func() {
		testOffsets := []int64{2}
		By("writing the blocks to the server")
		err := client.writeBlocksToServer(buf, testOffsets, file, &TestProgress{
			expectedStart:  2,
			expectedUpdate: 0,
		})
		Expect(err).ToNot(HaveOccurred())

		var sourceSize int64
		err = binary.Read(buf, binary.LittleEndian, &sourceSize)
		Expect(err).ToNot(HaveOccurred())
		Expect(sourceSize).To(Equal(int64(40)))

		var offset int64
		err = binary.Read(buf, binary.LittleEndian, &offset)
		Expect(err).ToNot(HaveOccurred())
		Expect(offset).To(Equal(int64(2)))

		offsetType := make([]byte, 1)
		n, err := buf.Read(offsetType)
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(1))
		Expect(offsetType[0]).To(Equal(Hole))

		_, err = buf.Read(make([]byte, 2))
		Expect(err).To(HaveOccurred())
	})

	It("writeBlocksToServer should write a block to the writer", func() {
		testOffsets := []int64{4}
		By("writing the blocks to the server")
		err := client.writeBlocksToServer(buf, testOffsets, file, nil)
		Expect(err).ToNot(HaveOccurred())

		var sourceSize int64
		err = binary.Read(buf, binary.LittleEndian, &sourceSize)
		Expect(err).ToNot(HaveOccurred())
		Expect(sourceSize).To(Equal(int64(40)))

		var offset int64
		err = binary.Read(buf, binary.LittleEndian, &offset)
		Expect(err).ToNot(HaveOccurred())
		Expect(offset).To(Equal(int64(4)))

		offsetType := make([]byte, 1)
		n, err := buf.Read(offsetType)
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(1))
		Expect(offsetType[0]).To(Equal(Block))

		res := make([]byte, 2)
		n, err = buf.Read(res)
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(2))
		Expect(res).To(Equal([]byte{3, 4}))
		_, err = buf.Read(make([]byte, 2))
		Expect(err).To(HaveOccurred())
	})

	It("should handle first error properly", func() {
		testOffsets := []int64{4}
		By("writing the blocks to the server")
		err := client.writeBlocksToServer(&ErrorWriter{
			buf:                  bytes.NewBuffer([]byte{}),
			writeUntilErrorCount: 0,
			currentCount:         0,
		}, testOffsets, file, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("error"))
	})

	It("should handle second error properly", func() {
		testOffsets := []int64{4}
		By("writing the blocks to the server")
		err := client.writeBlocksToServer(&ErrorWriter{
			buf:                  bytes.NewBuffer([]byte{}),
			writeUntilErrorCount: 1,
			currentCount:         0,
		}, testOffsets, file, &TestProgress{
			expectedStart:  2,
			expectedUpdate: 2,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("error"))
	})

	It("should handle error writing holes type properly", func() {
		testOffsets := []int64{2}
		By("writing the blocks to the server")
		err := client.writeBlocksToServer(&ErrorWriter{
			buf:                  bytes.NewBuffer([]byte{}),
			writeUntilErrorCount: 2,
			currentCount:         0,
		}, testOffsets, file, &TestProgress{
			expectedStart:  2,
			expectedUpdate: 2,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("error"))
	})

	It("should handle error writing block type properly", func() {
		testOffsets := []int64{4}
		By("writing the blocks to the server")
		err := client.writeBlocksToServer(&ErrorWriter{
			buf:                  bytes.NewBuffer([]byte{}),
			writeUntilErrorCount: 2,
			currentCount:         0,
		}, testOffsets, file, &TestProgress{
			expectedStart:  2,
			expectedUpdate: 2,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("error"))
	})

	It("should handle error writing block", func() {
		testOffsets := []int64{4}
		By("writing the blocks to the server")
		err := client.writeBlocksToServer(&ErrorWriter{
			buf:                  bytes.NewBuffer([]byte{}),
			writeUntilErrorCount: 3,
			currentCount:         0,
		}, testOffsets, file, &TestProgress{
			expectedStart:  2,
			expectedUpdate: 2,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("error"))
	})

	Context("with server", func() {
		It("should not detect differences between same files", func() {
			opts := BlockRsyncOptions{
				BlockSize:     64 * 1024,
				Preallocation: false,
			}
			port, err := getFreePort()
			Expect(err).ToNot(HaveOccurred())
			client = NewBlockrsyncClient(filepath.Join(testImagePath, testFileName), "localhost", port, &opts, GinkgoLogr.WithName("client"))
			server := NewBlockrsyncServer(filepath.Join(testImagePath, testFileName), port, &opts, GinkgoLogr.WithName("server"))
			go func() {
				defer GinkgoRecover()
				err := server.StartServer()
				Expect(err).ToNot(HaveOccurred())
			}()
			err = client.ConnectToTarget()
			Expect(err).ToNot(HaveOccurred())
			// Should not error, if trying to write it will error since no permissions.
		})

		It("should detect differences between source and empty file", func() {
			tmpDir, err := os.MkdirTemp("", "blockrsync")
			Expect(err).ToNot(HaveOccurred())
			opts := BlockRsyncOptions{
				BlockSize:     64 * 1024,
				Preallocation: false,
			}
			port, err := getFreePort()
			Expect(err).ToNot(HaveOccurred())
			client = NewBlockrsyncClient(filepath.Join(testImagePath, testFileName), "localhost", port, &opts, GinkgoLogr.WithName("client"))
			server := NewBlockrsyncServer(filepath.Join(tmpDir, testFileNameEmpty), port, &opts, GinkgoLogr.WithName("server"))
			go func() {
				defer GinkgoRecover()
				err := server.StartServer()
				Expect(err).ToNot(HaveOccurred())
			}()
			err = client.ConnectToTarget()
			Expect(err).ToNot(HaveOccurred())
			md5sum := md5.New()
			testFile, err := os.Open(filepath.Join(testImagePath, testFileName))
			Expect(err).ToNot(HaveOccurred())
			defer testFile.Close()
			_, err = io.Copy(md5sum, testFile)
			Expect(err).ToNot(HaveOccurred())
			hash := md5sum.Sum(nil)
			Expect(hex.EncodeToString(hash)).To(Equal(testMD5))
		})
	})
})

type ErrorWriter struct {
	buf                  *bytes.Buffer
	writeUntilErrorCount int
	currentCount         int
}

func (e *ErrorWriter) Write(p []byte) (n int, err error) {
	if e.currentCount == e.writeUntilErrorCount {
		return 0, errors.New("error")
	}
	e.currentCount++
	return e.buf.Write(p)
}

type TestProgress struct {
	expectedStart  int64
	expectedUpdate int64
}

func (p *TestProgress) Start(size int64) {
	Expect(size).To(Equal(p.expectedStart))
}

func (p *TestProgress) Update(pos int64) {
	Expect(pos).To(Equal(p.expectedUpdate))
}

func getFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}
