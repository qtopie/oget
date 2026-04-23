package oget

import (
	"io"
	"os"
	"testing"
)

func TestMmapStorageHandler(t *testing.T) {
	fileName := "test_mmap"
	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fileName)
	defer file.Close()

	size := int64(100)
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}

	handler, err := NewMmapStorageHandler(file, size)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()

	// Test WriteAt and ReadAt
	data := []byte("hello")
	n, err := handler.WriteAt(data, 10)
	if err != nil || n != 5 {
		t.Errorf("WriteAt failed: n=%d, err=%v", n, err)
	}

	buf := make([]byte, 5)
	n, err = handler.ReadAt(buf, 10)
	if err != nil || n != 5 {
		t.Errorf("ReadAt failed: n=%d, err=%v", n, err)
	}
	if string(buf) != "hello" {
		t.Errorf("got %q, want %q", string(buf), "hello")
	}

	// Test boundary - WriteAt past end
	dataLarge := []byte("this is too long for the end")
	n, err = handler.WriteAt(dataLarge, 90)
	if err != io.ErrShortWrite {
		t.Errorf("WriteAt past end should return io.ErrShortWrite, got %v", err)
	}
	if n != 10 {
		t.Errorf("got n=%d, want 10", n)
	}

	// Test boundary - ReadAt past end
	bufLarge := make([]byte, 30)
	n, err = handler.ReadAt(bufLarge, 80)
	if err != io.EOF {
		t.Errorf("ReadAt past end should return io.EOF, got %v", err)
	}
	if n != 20 {
		t.Errorf("got n=%d, want 20", n)
	}
}
