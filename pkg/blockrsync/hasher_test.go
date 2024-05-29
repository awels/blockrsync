package blockrsync

import (
	"bytes"
	"io"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testImagePath = "../../test_images"
	testFileName  = "cirros.raw"
	testFileSize  = 46137344
)

var _ = Describe("hasher tests", func() {
	var (
		hasher Hasher
	)

	BeforeEach(func() {
		hasher = NewFileHasher(DefaultBlockSize, GinkgoLogr.WithName("hasher"))
		Expect(hasher).ToNot(BeNil())
		Expect(hasher.BlockSize()).To(Equal(DefaultBlockSize))
	})

	It("should properly find the file size", func() {
		fileSize, err := hasher.(*FileHasher).getFileSize(filepath.Join(testImagePath, testFileName))
		Expect(err).To(BeNil())
		Expect(fileSize).To(Equal(int64(testFileSize)))
	})

	It("should error on invalid file", func() {
		size, err := hasher.(*FileHasher).getFileSize("invalid")
		Expect(err).ToNot(BeNil())
		Expect(size).To(Equal(int64(0)))
	})

	DescribeTable("should determine concurrency based on file size to block size ratio", func(fileSize, blockSize int64, expectedConcurrency int) {
		hasher = NewFileHasher(blockSize, GinkgoLogr.WithName("hasher"))
		concurrency := hasher.(*FileHasher).concurrentHashCount(fileSize)
		Expect(concurrency).To(Equal(expectedConcurrency))
	}, Entry("file size > 25 * block size", int64(testFileSize), int64(4096), defaultConcurrency),
		Entry("file size = block size", int64(4096), int64(4096), 1),
		Entry("file size < block size", int64(40960), int64(4096), 10),
	)

	It("should calculate the hashes of a file", func() {
		n, err := hasher.HashFile(filepath.Join(testImagePath, testFileName))
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(int64(testFileSize)))
		Expect(hasher.GetHashes()).To(HaveLen(int(testFileSize / DefaultBlockSize)))
	})

	It("should serialize and deserialize hashes", func() {
		n, err := hasher.HashFile(filepath.Join(testImagePath, testFileName))
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(int64(testFileSize)))
		var b bytes.Buffer
		w := io.Writer(&b)
		err = hasher.SerializeHashes(w)
		Expect(err).ToNot(HaveOccurred())
		hashes := hasher.GetHashes()
		// 16 for the blocksize and length, 72 for each hash
		Expect(b.Len()).To(Equal(72*len(hashes) + 16))
		r := io.Reader(&b)
		blockSize, h, err := hasher.DeserializeHashes(r)
		Expect(err).ToNot(HaveOccurred())
		Expect(blockSize).To(Equal(DefaultBlockSize))
		Expect(h).To(HaveLen(len(hashes)))
	})

	getCirrosHashes := func() map[int64][]byte {
		cirrosHasher := NewFileHasher(DefaultBlockSize, GinkgoLogr.WithName("cirros hasher"))
		n, err := cirrosHasher.HashFile(filepath.Join(testImagePath, testFileName))
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(int64(testFileSize)))
		return cirrosHasher.GetHashes()
	}

	getCirrosHashesModified := func() map[int64][]byte {
		res := getCirrosHashes()
		res[0] = []byte("modified")
		return res
	}

	getCirrosHashesEntryRemoved := func() map[int64][]byte {
		res := getCirrosHashes()
		delete(res, 0)
		return res
	}

	getLargerCirrosHashes := func() map[int64][]byte {
		res := getCirrosHashes()
		res[DefaultBlockSize*1000] = []byte("modified")
		return res
	}

	DescribeTable("It should properly determine differences between hashes", func(cmpHash map[int64][]byte, expected []int64) {
		n, err := hasher.HashFile(filepath.Join(testImagePath, testFileName))
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(int64(testFileSize)))
		diff, err := hasher.DiffHashes(DefaultBlockSize, cmpHash)
		Expect(err).ToNot(HaveOccurred())
		Expect(diff).To(Equal(expected))
	},
		Entry("no differences", getCirrosHashes(), nil),
		Entry("single differences", getCirrosHashesModified(), []int64{0}),
		Entry("single differences, removed", getCirrosHashesEntryRemoved(), []int64{0}),
		Entry("larger comparison, should strip", getLargerCirrosHashes(), nil),
	)

	It("should fail if block size is different", func() {
		n, err := hasher.HashFile(filepath.Join(testImagePath, testFileName))
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(int64(testFileSize)))
		_, err = hasher.DiffHashes(int64(4096), nil)
		Expect(err).To(HaveOccurred())
	})
})
