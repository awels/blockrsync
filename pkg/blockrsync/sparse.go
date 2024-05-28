package blockrsync

import (
	"errors"
	"os"
	"syscall"
)

const (
	FALLOC_FL_KEEP_SIZE  = 0x01 /* default is extend size */
	FALLOC_FL_PUNCH_HOLE = 0x02 /* de-allocates range */
)

var (
	ErrPunchHoleNotSupported = errors.New("this filesystem does not support punching holes. Use xfs, ext4, btrfs or such")
)

func PunchHole(f *os.File, offset, size int64) error {
	err := syscall.Fallocate(int(f.Fd()), FALLOC_FL_KEEP_SIZE|FALLOC_FL_PUNCH_HOLE, offset, size)

	if err == syscall.ENOTSUP {
		err = ErrPunchHoleNotSupported
	}

	return err
}
