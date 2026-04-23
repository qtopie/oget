//go:build !linux
// +build !linux

package oget

import "errors"

const (
	tcpCongestion = 0
	spliceFMove   = 0
	spliceFMore   = 0
)

func splice(rfd int, roff *int64, wfd int, woff *int64, len int, flags int) (int, error) {
	return 0, errors.New("splice is only supported on linux")
}

func fallocate(fd int, mode uint32, off int64, len int64) error {
	return errors.New("fallocate is only supported on linux")
}
