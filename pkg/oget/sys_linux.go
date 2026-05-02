//go:build linux
// +build linux

package oget

import (
	"os"

	"golang.org/x/sys/unix"
)

const (
	tcpCongestion = unix.TCP_CONGESTION
	spliceFMove   = unix.SPLICE_F_MOVE
	spliceFMore   = unix.SPLICE_F_MORE
)

func splice(rfd int, roff *int64, wfd int, woff *int64, len int, flags int) (int64, error) {
	return unix.Splice(rfd, roff, wfd, woff, len, flags)
}

func fallocate(fd int, mode uint32, off int64, len int64) error {
	return unix.Fallocate(fd, mode, off, len)
}

func setBBR(fd uintptr) {
	_ = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION, "bbr")
}

func mmapFileOffset(f *os.File, length int, offset int64) ([]byte, error) {
	return unix.Mmap(int(f.Fd()), offset, length, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
}

func munmapFile(data []byte) error {
	return unix.Munmap(data)
}
