package blockrsync

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("progress tests", func() {
	It("should properly update progress", func() {
		p := progress{
			logger: GinkgoLogr.WithName("progress"),
		}
		p.Start(100)
		p.Update(50)
		Expect(p.current).To(Equal(int64(50)))
		time.Sleep(time.Second)
		p.Update(100)
		Expect(p.current).To(Equal(int64(100)))
	})
})
