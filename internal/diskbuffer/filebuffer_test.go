package diskbuffer

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFileBuffer_Basic(t *testing.T) {
	fb, err := newFileBuffer(100, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := fb.close(); err != nil {
			t.Errorf("fb.close() error = %v", err)
		}
	}()

	if got, want := fb.Size(), int64(0); got != want {
		t.Errorf("fb.Size() = %#v, want %#v", got, want)
	}
	if got, want := fb.Limit(), int64(100); got != want {
		t.Errorf("fb.Limit() = %#v, want %#v", got, want)
	}

	n, err := fb.write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 5; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := fb.Size(), int64(5); got != want {
		t.Errorf("fb.Size() = %#v, want %#v", got, want)
	}
}

func TestFileBuffer_Write_ExceedsLimit(t *testing.T) {
	fb, err := newFileBuffer(5, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := fb.close(); err != nil {
			t.Errorf("fb.close() error = %v", err)
		}
	}()

	// Write more than limit
	n, err := fb.write([]byte("hello world"))
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
	if got, want := n, 5; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	} // partial write

	// Writing when no free space
	n, err = fb.write([]byte("x"))
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
	if got, want := n, 0; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
}

func TestFileBuffer_ReadAt(t *testing.T) {
	fb, err := newFileBuffer(100, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := fb.close(); err != nil {
			t.Errorf("fb.close() error = %v", err)
		}
	}()

	_, _ = fb.write([]byte("ABCDE"))

	buf := make([]byte, 3)
	n, rerr := fb.readAt(2, buf)
	if err := rerr; err != nil {
		t.Fatalf("rerr error = %v", err)
	}
	if got, want := n, 3; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := string(buf), "CDE"; got != want {
		t.Errorf("string(buf) = %#v, want %#v", got, want)
	}

	// Empty buffer case
	fb2, err := newFileBuffer(100, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := fb2.close(); err != nil {
			t.Errorf("fb2.close() error = %v", err)
		}
	}()

	_, rerr = fb2.readAt(0, buf)
	if got, want := rerr, io.EOF; !errors.Is(got, want) {
		t.Errorf("rerr = %v, want an error matching %v", got, want)
	}
}

func TestFileBuffer_ReadByteAt(t *testing.T) {
	fb, err := newFileBuffer(100, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := fb.close(); err != nil {
			t.Errorf("fb.close() error = %v", err)
		}
	}()

	_, _ = fb.write([]byte("XY"))
	c, rerr := fb.readByteAt(0)
	if err := rerr; err != nil {
		t.Fatalf("rerr error = %v", err)
	}
	if got, want := c, byte('X'); got != want {
		t.Errorf("c = %#v, want %#v", got, want)
	}

	c, rerr = fb.readByteAt(1)
	if err := rerr; err != nil {
		t.Fatalf("rerr error = %v", err)
	}
	if got, want := c, byte('Y'); got != want {
		t.Errorf("c = %#v, want %#v", got, want)
	}
}

func TestFileBuffer_WriteToAt(t *testing.T) {
	fb, err := newFileBuffer(100, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := fb.close(); err != nil {
			t.Errorf("fb.close() error = %v", err)
		}
	}()

	_, _ = fb.write([]byte("ABCDE"))

	var out bytes.Buffer
	n, werr := fb.writeToAt(2, &out)
	if err := werr; err != nil {
		t.Fatalf("werr error = %v", err)
	}
	if got, want := n, int64(3); got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := out.String(), "CDE"; got != want {
		t.Errorf("out.String() = %#v, want %#v", got, want)
	}

	// Negative offset
	_, werr = fb.writeToAt(-1, &out)
	if got, want := werr, io.EOF; !errors.Is(got, want) {
		t.Errorf("werr = %v, want an error matching %v", got, want)
	}

	// Beyond size
	_, werr = fb.writeToAt(10, &out)
	if got, want := werr, io.EOF; !errors.Is(got, want) {
		t.Errorf("werr = %v, want an error matching %v", got, want)
	}
}

func TestFileBuffer_Close_Nil(t *testing.T) {
	fb, err := newFileBuffer(100, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = fb.close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second close (file is nil)
	err = fb.close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
