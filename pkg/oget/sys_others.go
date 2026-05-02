//go:build !linux
// +build !linux

package oget

import (
	"errors"
	"os"
)

const (
	tcpCongestion = 0
	spliceFMove   = 0
	spliceFMore   = 0
)

func splice(rfd int, roff *int64, wfd int, woff *int64, len int, flags int) (int64, error) {
	return 0, errors.New("splice is only supported on linux")
}

func fallocate(fd int, mode uint32, off int64, len int64) error {
	return errors.New("fallocate is only supported on linux")
}

func setBBR(fd uintptr) {
	// Not supported on other platforms
}

func mmapFileOffset(f *os.File, length int, offset int64) ([]byte, error) {
	return nil, errors.New("mmap is only supported on linux")
}

func munmapFile(data []byte) error {
	return errors.New("munmap is only supported on linux")
}

func NewMmapStorageHandler(file *os.File, length int64) (StorageHandler, error) {
	return nil, errors.New("mmap storage is only supported on linux")
}
