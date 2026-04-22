package oget

import (
	"io"
	"os"
)

// URingStorageHandler is currently a placeholder to ensure the project builds.
// In this environment, it delegates to FileStorageHandler.
type URingStorageHandler struct {
	FileStorageHandler
}

// NewURingStorageHandler creates a placeholder for uring storage.
func NewURingStorageHandler(file *os.File, entries uint) (*URingStorageHandler, error) {
	return &URingStorageHandler{
		FileStorageHandler: FileStorageHandler{File: file},
	}, nil
}

func (h *URingStorageHandler) ReadAtFrom(r io.Reader, off int64, count int64) (int64, error) {
	return h.FileStorageHandler.ReadAtFrom(r, off, count)
}

func (h *URingStorageHandler) SpliceFrom(fd uintptr, off int64, count int64) (int64, error) {
	return h.FileStorageHandler.SpliceFrom(fd, off, count)
}
