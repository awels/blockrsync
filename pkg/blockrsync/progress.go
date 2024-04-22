package blockrsync

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

type progress struct {
	total        int64
	current      int64
	progressType string
	lastUpdate   time.Time
	logger       logr.Logger
}

func (p *progress) Start(size int64) {
	p.total = size
	p.current = int64(0)
	p.lastUpdate = time.Now()
	p.logger.Info(fmt.Sprintf("%s total size %d", p.progressType, p.total))
}

func (p *progress) Update(pos int64) {
	p.current = pos
	if time.Since(p.lastUpdate).Seconds() > time.Second.Seconds() {
		p.logger.Info(fmt.Sprintf("%s %.2f%%", p.progressType, (float64(p.current) / float64(p.total) * 100)))
		p.lastUpdate = time.Now()
	}
}
