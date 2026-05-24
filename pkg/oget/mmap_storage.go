//go:build linux || darwin
// +build linux darwin

package oget

import (
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// MmapStorageHandler implements StorageHandler using memory-mapped files.
type MmapStorageHandler struct {
	file *os.File
	data []byte
	mu   sync.RWMutex
}

// NewMmapStorageHandler creates a new mmap-based storage backend.
func NewMmapStorageHandler(file *os.File, length int64) (*MmapStorageHandler, error) {
	if length <= 0 {
		return nil, fmt.Errorf("mmap requires a positive file length")
	}

	data, err := syscall.Mmap(int(file.Fd()), 0, int(length), syscall.PROT_WRITE|syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}

	return &MmapStorageHandler{
		file: file,
		data: data,
	}, nil
}

func (h *MmapStorageHandler) WriteAt(p []byte, off int64) (n int, err error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if off < 0 || int(off) >= len(h.data) {
		return 0, io.ErrShortWrite
	}

	end := int(off) + len(p)
	if end > len(h.data) {
		n = copy(h.data[off:], p[:len(h.data)-int(off)])
		return n, io.ErrShortWrite
	}

	n = copy(h.data[off:end], p)
	return n, nil
}

func (h *MmapStorageHandler) ReadAt(p []byte, off int64) (n int, err error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if off < 0 || int(off) >= len(h.data) {
		return 0, io.EOF
	}

	end := int(off) + len(p)
	if end > len(h.data) {
		n = copy(p, h.data[off:])
		return n, io.EOF
	}

	n = copy(p, h.data[off:end])
	return n, nil
}

func (h *MmapStorageHandler) ReadAtFrom(r io.Reader, off int64, count int64) (n int64, err error) {
	if off < 0 || int(off) >= len(h.data) {
		return 0, io.EOF
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	limit := int(off) + int(count)
	if limit > len(h.data) {
		limit = len(h.data)
	}

	rn, err := io.ReadFull(r, h.data[off:limit])
	return int64(rn), err
}

func (h *MmapStorageHandler) SpliceFrom(fd uintptr, off int64, count int64) (int64, error) {
	p1, p2, err := os.Pipe()
	if err != nil {
		return 0, err
	}
	defer p1.Close()
	defer p2.Close()

	var total int64
	for total < count {
		n1, err := splice(int(fd), nil, int(p2.Fd()), nil, int(count-total), spliceFMove|spliceFMore)
		if err != nil {
			return total, err
		}
		if n1 == 0 {
			break
		}
		n2, err := splice(int(p1.Fd()), nil, int(h.file.Fd()), &off, int(n1), spliceFMove)
		if err != nil {
			return total, err
		}
		total += int64(n2)
	}
	return total, nil
}

func (h *MmapStorageHandler) Seek(offset int64, whence int) (int64, error) {
	return h.file.Seek(offset, whence)
}

func (h *MmapStorageHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.data != nil {
		if err := syscall.Munmap(h.data); err != nil {
			return err
		}
		h.data = nil
	}
	return h.file.Close()
}

func (h *MmapStorageHandler) Sync() error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.data == nil {
		return nil
	}
	_, _, err := syscall.Syscall(syscall.SYS_MSYNC, uintptr(unsafe.Pointer(&h.data[0])), uintptr(len(h.data)), syscall.MS_SYNC)
	if err != 0 {
		return err
	}
	return nil
}
