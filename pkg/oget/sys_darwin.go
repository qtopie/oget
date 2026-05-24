//go:build darwin
// +build darwin

package oget

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	tcpCongestion = 0 // Not supported on Darwin via setsockopt
	spliceFMove   = 0
	spliceFMore   = 0
)

func splice(rfd int, roff *int64, wfd int, woff *int64, len int, flags int) (int64, error) {
	return 0, syscall.ENOTSUP
}

func fallocate(fd int, mode uint32, off int64, len int64) error {
	// macOS doesn't have fallocate. Use F_PREALLOCATE if needed, 
	// but Ftruncate is a reasonable fallback for ensuring size.
	return unix.Ftruncate(fd, len)
}

func setBBR(fd uintptr) {
	// Not supported on Darwin
}

func mmapFileOffset(f *os.File, length int, offset int64) ([]byte, error) {
	return unix.Mmap(int(f.Fd()), offset, length, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
}

func munmapFile(data []byte) error {
	return unix.Munmap(data)
}
