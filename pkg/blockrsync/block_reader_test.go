package blockrsync

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	blockReader = "block reader"
)

var _ = Describe(blockReader, func() {
	It("should read from a reader", func() {
		r := createBytesReader(4)
		br := NewBlockReader(r, 4, GinkgoLogr.WithName(blockReader))
		Expect(br).ToNot(BeNil())
		cont, err := br.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(cont).To(BeTrue())
		Expect(br.Block()).To(HaveLen(4))
		for i := 0; i < 4; i++ {
			Expect(br.Block()[i]).To(Equal(byte(i)), "%v", br.Block())
		}
		Expect(br.Offset()).To(Equal(int64(4096)))
	})

	It("should handle errors", func() {
		cont, err := handleReadError(errors.New("Random error"), nocallback)
		Expect(err).To(HaveOccurred())
		Expect(cont).To(BeFalse())
		cont, err = handleReadError(io.EOF, nocallback)
		Expect(err).ToNot(HaveOccurred())
		Expect(cont).To(BeFalse())
		cont, err = handleReadError(io.ErrUnexpectedEOF, nocallback)
		Expect(err).ToNot(HaveOccurred())
		Expect(cont).To(BeFalse())
	})

	It("should handle not receiving offset data", func() {
		b := []byte{}
		buf := bytes.NewBuffer(b)
		buf.Write([]byte{1})
		br := NewBlockReader(buf, 4, GinkgoLogr.WithName(blockReader))
		Expect(br).ToNot(BeNil())
		cont, err := br.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(cont).To(BeFalse())
		Expect(br.Offset()).To(Equal(int64(0)))
	})

	It("should handle not receiving offset type data", func() {
		b := []byte{}
		buf := bytes.NewBuffer(b)
		err := binary.Write(buf, binary.LittleEndian, int64(4096))
		Expect(err).ToNot(HaveOccurred())
		br := NewBlockReader(buf, 4, GinkgoLogr.WithName(blockReader))
		Expect(br).ToNot(BeNil())
		cont, err := br.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(cont).To(BeFalse())
		Expect(br.Offset()).To(Equal(int64(4096)))
	})

	It("should skip reading data if receiving a hole", func() {
		b := []byte{}
		buf := bytes.NewBuffer(b)
		err := binary.Write(buf, binary.LittleEndian, int64(4096))
		Expect(err).ToNot(HaveOccurred())
		buf.Write([]byte{Hole})
		br := NewBlockReader(buf, 4, GinkgoLogr.WithName(blockReader))
		Expect(br).ToNot(BeNil())
		cont, err := br.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(cont).To(BeTrue())
		Expect(br.Offset()).To(Equal(int64(4096)))
	})

	It("should handle not getting complete block data", func() {
		b := []byte{}
		buf := bytes.NewBuffer(b)
		err := binary.Write(buf, binary.LittleEndian, int64(4096))
		Expect(err).ToNot(HaveOccurred())
		buf.Write([]byte{Block})
		buf.Write([]byte{255})
		br := NewBlockReader(buf, 4, GinkgoLogr.WithName(blockReader))
		Expect(br).ToNot(BeNil())
		cont, err := br.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(cont).To(BeFalse())
		Expect(br.Offset()).To(Equal(int64(4096)))
		Expect(br.Block()).To(HaveLen(1))
		Expect(br.Block()[0]).To(Equal(byte(255)), "%v", br.Block())
	})
})

func createBytesReader(blockSize int) io.Reader {
	if blockSize > 255 {
		Fail("block size must be less than 256")
	}
	b := []byte{}
	buf := bytes.NewBuffer(b)
	err := binary.Write(buf, binary.LittleEndian, int64(4096))
	Expect(err).ToNot(HaveOccurred())
	buf.Write([]byte{1})
	for i := 0; i < blockSize; i++ {
		buf.Write([]byte{byte(i)})
	}
	fmt.Fprintf(GinkgoWriter, "buf: %v\n", buf.Bytes())
	return buf
}
