/*
 * Copyright 2020 National Library of Norway.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *       http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// nolint
package diskbuffer

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func createReaderOfSize(size int64) (io.Reader, string) {
	b := make([]byte, int(size))
	for i := range b {
		b[i] = byte(i*31 + 7) // deterministic
	}
	h := md5.Sum(b)
	return bytes.NewReader(b), hex.EncodeToString(h[:])
}

func hashOfReader(r io.Reader) string {
	h := md5.New()
	_, _ = io.Copy(h, r)
	return hex.EncodeToString(h.Sum(nil))
}

func TestSmallBuffer(t *testing.T) {
	r, hash := createReaderOfSize(1)
	bb := New()
	defer bb.Close()
	_, err := io.Copy(bb, r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
}

func TestBigBuffer(t *testing.T) {
	r, hash := createReaderOfSize(13631488)
	bb := New()
	defer bb.Close()
	_, err := io.Copy(bb, r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
}

func TestSeek(t *testing.T) {
	tlen := int64(1057576)
	r, hash := createReaderOfSize(tlen)
	bb := New()
	defer bb.Close()
	_, err := io.Copy(bb, r)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
	l := bb.Size()
	if got, want := l, tlen; got != want {
		t.Errorf("l = %#v, want %#v", got, want)
	}

	bb.Seek(0, io.SeekStart)
	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
	l = bb.Size()
	if got, want := l, tlen; got != want {
		t.Errorf("l = %#v, want %#v", got, want)
	}
}

func TestSeekWithFile(t *testing.T) {
	tlen := int64(1057576)
	r, hash := createReaderOfSize(tlen)
	bb := New(WithMaxMemBytes(1))
	defer bb.Close()
	_, err := io.Copy(bb, r)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
	l := bb.Size()
	if got, want := l, tlen; got != want {
		t.Errorf("l = %#v, want %#v", got, want)
	}

	bb.Seek(0, io.SeekStart)
	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
	l = bb.Size()
	if got, want := l, tlen; got != want {
		t.Errorf("l = %#v, want %#v", got, want)
	}
}

func TestLimitDoesNotExceed(t *testing.T) {
	requestSize := int64(1057576)
	r, hash := createReaderOfSize(requestSize)
	bb := New(WithMaxMemBytes(1024), WithMaxTotalBytes(requestSize+1))
	defer bb.Close()
	_, err := io.Copy(bb, r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
	size := bb.Size()
	if got, want := size, requestSize; got != want {
		t.Errorf("size = %#v, want %#v", got, want)
	}
}

func TestLimitExceeds(t *testing.T) {
	requestSize := int64(1057576)
	r, _ := createReaderOfSize(requestSize)
	bb := New(WithMaxMemBytes(1024), WithMaxTotalBytes(requestSize-1))
	defer bb.Close()
	_, err := io.Copy(bb, r)
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
}

func TestLimitExceedsMemBytes(t *testing.T) {
	requestSize := int64(1057576)
	r, _ := createReaderOfSize(requestSize)
	bb := New(WithMaxMemBytes(int(requestSize+1)), WithMaxTotalBytes(requestSize-1))
	defer bb.Close()
	_, err := io.Copy(bb, r)
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
}

func TestWriteToBigBuffer(t *testing.T) {
	l := int64(13631488)
	r, hash := createReaderOfSize(l)
	bb := New()
	defer bb.Close()
	_, err := io.Copy(bb, r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	other := &bytes.Buffer{}
	wrote, err := bb.WriteTo(other)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := wrote, l; got != want {
		t.Errorf("wrote = %#v, want %#v", got, want)
	}
	if got, want := hashOfReader(other), hash; got != want {
		t.Errorf("hashOfReader(other) = %#v, want %#v", got, want)
	}
}

func TestWriteToSmallBuffer(t *testing.T) {
	l := int64(1)
	r, hash := createReaderOfSize(l)
	bb := New()
	defer bb.Close()
	_, err := io.Copy(bb, r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	other := &bytes.Buffer{}
	wrote, err := bb.WriteTo(other)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := wrote, l; got != want {
		t.Errorf("wrote = %#v, want %#v", got, want)
	}
	if got, want := hashOfReader(other), hash; got != want {
		t.Errorf("hashOfReader(other) = %#v, want %#v", got, want)
	}
}

func TestReadFromSmallBuffer(t *testing.T) {
	r, hash := createReaderOfSize(1)

	bb := New()
	defer bb.Close()

	total, err := bb.ReadFrom(r)
	if got := err; got != nil {
		t.Errorf("err = %v, want nil", got)
	}
	if got, want := total, int64(1); got != want {
		t.Errorf("total = %#v, want %#v", got, want)
	}

	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
}

func TestReadFromBigBuffer(t *testing.T) {
	size := int64(13631488)
	r, hash := createReaderOfSize(size)

	bb := New()
	defer bb.Close()

	total, err := bb.ReadFrom(r)
	if got := err; got != nil {
		t.Errorf("err = %v, want nil", got)
	}
	if got, want := total, size; got != want {
		t.Errorf("total = %#v, want %#v", got, want)
	}

	if got, want := hashOfReader(bb), hash; got != want {
		t.Errorf("hashOfReader(bb) = %#v, want %#v", got, want)
	}
}

func TestWriterOnceMaxSizeExceeded(t *testing.T) {
	size := int64(1000)
	r, _ := createReaderOfSize(size)

	bb := New(WithMaxMemBytes(10), WithMaxTotalBytes(100))
	defer bb.Close()

	_, err := io.Copy(bb, r)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

func TestSeekInvalidWhence(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, _ = bb.Write([]byte("abc"))
	_, err := bb.Seek(0, 12345)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

func TestSeekBounds(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("abc"))

	_, err := bb.Seek(-1, io.SeekStart)
	if err == nil {
		t.Errorf("err: expected an error")
	}

	// Standard io.Seeker: seeking past end is allowed
	pos, err := bb.Seek(4, io.SeekStart)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := pos, int64(4); got != want {
		t.Errorf("pos = %#v, want %#v", got, want)
	}

	_, err = bb.Seek(-1, io.SeekEnd)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	} // valid: position size-1
}

func TestTotalLimitExactThenExceedByOne(t *testing.T) {
	bb := New(WithMaxMemBytes(4), WithMaxTotalBytes(5))
	defer bb.Close()

	n, err := bb.Write([]byte("12345"))
	if got, want := n, 5; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := bb.Size(), int64(5); got != want {
		t.Errorf("bb.Size() = %#v, want %#v", got, want)
	}

	n, err = bb.Write([]byte("x"))
	if got, want := n, 0; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
	if got, want := bb.Size(), int64(5); got != want {
		t.Errorf("bb.Size() = %#v, want %#v", got, want)
	}
}

func TestMemLimitBoundarySpillsToDisk(t *testing.T) {
	bb := New(WithMaxMemBytes(3), WithMaxTotalBytes(10))
	defer bb.Close()

	_, err := bb.Write([]byte("abc")) // fills mem
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_, err = bb.Write([]byte("def")) // should go to file
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	bb.Seek(0, io.SeekStart)
	out, _ := io.ReadAll(bb)
	if got, want := string(out), "abcdef"; got != want {
		t.Errorf("string(out) = %#v, want %#v", got, want)
	}
	if got, want := bb.Size(), int64(6); got != want {
		t.Errorf("bb.Size() = %#v, want %#v", got, want)
	}
}

func TestReadAtAcrossMemDiskBoundary(t *testing.T) {
	bb := New(WithMaxMemBytes(4), WithMaxTotalBytes(20))
	defer bb.Close()

	_, err := bb.Write([]byte("0123456789")) // mem:0123 disk:456789
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	b, ok := bb.(*buffer)
	if got := ok; !got {
		t.Errorf("ok = %v, want true", got)
	}

	p := make([]byte, 6)
	n, err := b.ReadAt(p, 2) // "234567"
	if got, want := n, 6; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := string(p), "234567"; got != want {
		t.Errorf("string(p) = %#v, want %#v", got, want)
	}
}

func TestReadByteAtBoundary(t *testing.T) {
	bb := New(WithMaxMemBytes(4))
	defer bb.Close()
	_, _ = bb.Write([]byte("012345"))

	b, ok := bb.(*buffer)
	if got := ok; !got {
		t.Errorf("ok = %v, want true", got)
	}

	c, err := b.ReadByteAt(3) // last in mem
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := c, byte('3'); got != want {
		t.Errorf("c = %#v, want %#v", got, want)
	}

	c, err = b.ReadByteAt(4) // first in disk
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := c, byte('4'); got != want {
		t.Errorf("c = %#v, want %#v", got, want)
	}

	_, err = b.ReadByteAt(6) // out of range
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %#v, want %#v", got, want)
	}
}

func TestPeekDoesNotAdvanceAndEOF(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("abc"))

	p, err := bb.Peek(2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := string(p), "ab"; got != want {
		t.Errorf("string(p) = %#v, want %#v", got, want)
	}
	if got, want := bb.(*buffer).pos, int64(0); got != want {
		t.Errorf("bb.(*buffer).pos = %#v, want %#v", got, want)
	} // if you can access; otherwise read and compare

	buf := make([]byte, 2)
	n, err := bb.Read(buf)
	if got, want := n, 2; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := string(buf), "ab"; got != want {
		t.Errorf("string(buf) = %#v, want %#v", got, want)
	}

	p, err = bb.Peek(5)
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got := len(p) <= 5; !got {
		t.Errorf("len(p) <= 5 = %v, want true", got)
	} // if you return short slices, this becomes exact
}

func TestReadFromExceedsTotalLimitReportsCount(t *testing.T) {
	bb := New(WithMaxMemBytes(4), WithMaxTotalBytes(6))
	defer bb.Close()

	r := bytes.NewReader([]byte("0123456789"))
	n, err := bb.ReadFrom(r)
	if got, want := n, int64(6); got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
	if got, want := bb.Size(), int64(6); got != want {
		t.Errorf("bb.Size() = %#v, want %#v", got, want)
	}
}

func TestReadOnlyRejectsWrites(t *testing.T) {
	bb := New(WithReadOnly(true))
	defer bb.Close()

	_, err := bb.Write([]byte("x"))
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	err = bb.WriteByte('x')
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.WriteString("x")
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.ReadFrom(bytes.NewReader([]byte("x")))
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestCloseIdempotent(t *testing.T) {
	bb := New(WithMaxMemBytes(1))
	_ = bb.Close()
	err := bb.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUseAfterClose(t *testing.T) {
	bb := New()
	_, _ = bb.Write([]byte("hello"))
	if err := bb.Close(); err != nil {
		t.Fatalf("bb.Close() error = %v", err)
	}

	// All operations should return ErrClosed
	_, err := bb.Write([]byte("x"))
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.WriteString("x")
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	err = bb.WriteByte('x')
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.ReadFrom(strings.NewReader("x"))
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.Seek(0, io.SeekStart)
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.Read(make([]byte, 1))
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.ReadAt(make([]byte, 1), 0)
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.ReadByte()
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.ReadBytes('\n')
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.Peek(1)
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.WriteTo(io.Discard)
	if got, want := err, ErrClosed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	// Size/Len/Limit return 0
	if got, want := bb.Size(), int64(0); got != want {
		t.Errorf("bb.Size() = %#v, want %#v", got, want)
	}
}

func TestPeekNegativeCount(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("abc"))

	_, err := bb.Peek(-1)
	if got, want := err, errNegativeCount; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestSliceFiniteSize(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("0123456789"))

	s := io.NewSectionReader(bb, 3, 4) // "3456"
	out, err := io.ReadAll(s)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := string(out), "3456"; got != want {
		t.Errorf("string(out) = %#v, want %#v", got, want)
	}
}

func TestSliceSeekEnd(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("0123456789"))

	s := io.NewSectionReader(bb, 3, 4) // size 4
	_, err := s.Seek(-1, io.SeekEnd)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	b := make([]byte, 1)
	n, err := s.Read(b)
	if got, want := n, 1; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := string(b), "6"; got != want {
		t.Errorf("string(b) = %#v, want %#v", got, want)
	}
}

func TestBuffer_EmptyBuffer(t *testing.T) {
	bb := New()
	defer bb.Close()

	// Size should be 0
	if bb.Size() != 0 {
		t.Errorf("Empty buffer Size() = %d, want 0", bb.Size())
	}

	// Read should return EOF
	buf := make([]byte, 10)
	n, err := bb.Read(buf)
	if err != io.EOF {
		t.Errorf("Read() on empty buffer should return EOF, got: %v", err)
	}
	if n != 0 {
		t.Errorf("Read() on empty buffer returned %d bytes, want 0", n)
	}
}

func TestBuffer_SingleByte(t *testing.T) {
	bb := New()
	defer bb.Close()

	// Write single byte
	n, err := bb.Write([]byte{42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 1; got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}

	// Seek to start
	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read single byte
	buf := make([]byte, 1)
	n, err = bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 1; got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}
	if got, want := buf[0], byte(42); got != want {
		t.Fatalf("buf[0] = %#v, want %#v", got, want)
	}
}

func TestBuffer_ZeroByteWrite(t *testing.T) {
	bb := New()
	defer bb.Close()

	// Write zero bytes
	n, err := bb.Write([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 0; got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}

	// Size should still be 0
	if bb.Size() != 0 {
		t.Errorf("After zero-byte write, Size() = %d, want 0", bb.Size())
	}
}

func TestBuffer_ZeroByteRead(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, err := bb.Write([]byte("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read zero bytes
	buf := make([]byte, 0)
	n, err := bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 0; got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}
}

func TestBuffer_SeekBeyondEnd(t *testing.T) {
	bb := New()
	defer bb.Close()

	data := []byte("test data")
	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Standard io.Seeker: seeking past end is allowed
	pos, err := bb.Seek(100, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := pos, int64(100); got != want {
		t.Errorf("pos = %#v, want %#v", got, want)
	}

	// Read at past-end position returns 0, io.EOF
	buf := make([]byte, 10)
	n, readErr := bb.Read(buf)
	if got, want := n, 0; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := readErr, io.EOF; !errors.Is(got, want) {
		t.Errorf("readErr = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_SeekNegative(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, err := bb.Write([]byte("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Seek to negative position
	_, err = bb.Seek(-1, io.SeekStart)
	if err == nil {
		t.Error("Seek to negative position should return error")
	}
}

func TestBuffer_SeekCurrent(t *testing.T) {
	bb := New()
	defer bb.Close()

	data := []byte("0123456789")
	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Seek to start
	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read 5 bytes
	buf := make([]byte, 5)
	_, err = bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Seek forward 2 from current
	pos, err := bb.Seek(2, io.SeekCurrent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos != 7 {
		t.Errorf("Seek(2, Current) from position 5 = %d, want 7", pos)
	}

	// Read should get byte at position 7
	buf = make([]byte, 1)
	n, err := bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 1; got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}
	if buf[0] != '7' {
		t.Errorf("Read after seek = %c, want '7'", buf[0])
	}
}

func TestBuffer_SeekEnd(t *testing.T) {
	bb := New()
	defer bb.Close()

	data := []byte("0123456789")
	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Seek to end
	pos, err := bb.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos != int64(len(data)) {
		t.Errorf("Seek(0, End) = %d, want %d", pos, len(data))
	}

	// Seek backward from end
	pos, err = bb.Seek(-5, io.SeekEnd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos != int64(len(data)-5) {
		t.Errorf("Seek(-5, End) = %d, want %d", pos, len(data)-5)
	}

	// Read should get the last 5 bytes
	buf := make([]byte, 5)
	n, err := bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 5; got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}
	if string(buf) != "56789" {
		t.Errorf("Read after Seek(-5, End) = %q, want %q", string(buf), "56789")
	}
}

func TestBuffer_MultipleClose(t *testing.T) {
	bb := New()

	// First close
	err := bb.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second close should not panic
	err = bb.Close()
	if err != nil {
		t.Logf("Second Close() returned error (may be acceptable): %v", err)
	}
}

func TestBuffer_WriteAfterClose(t *testing.T) {
	bb := New()
	bb.Close()

	// Write after close - behavior is implementation-specific
	_, err := bb.Write([]byte("test"))
	t.Logf("Write() after Close() returned: %v", err)
}

func TestBuffer_ReadAfterClose(t *testing.T) {
	bb := New()
	_, _ = bb.Write([]byte("test"))
	bb.Close()

	// Read after close - behavior is implementation-specific
	buf := make([]byte, 10)
	_, err := bb.Read(buf)
	t.Logf("Read() after Close() returned: %v", err)
}

func TestBuffer_ConcurrentWrites(t *testing.T) {
	for _, memLimit := range []int{0, 32, 1024} {
		t.Run(fmt.Sprint(memLimit), func(t *testing.T) {
			bb := New(WithMaxMemBytes(memLimit), WithTmpDir(t.TempDir()))
			defer bb.Close()
			results := make(chan error, 2)
			start := make(chan struct{})
			for _, write := range []func() (int, error){
				func() (int, error) { return bb.Write([]byte("a")) },
				func() (int, error) { return bb.WriteString("b") },
			} {
				go func() {
					<-start
					for i := 0; i < 100; i++ {
						n, err := write()
						if err != nil {
							results <- err
							return
						}
						if n != 1 {
							results <- io.ErrShortWrite
							return
						}
					}
					results <- nil
				}()
			}
			close(start)
			// Join both writers before inspecting or closing the buffer.
			err1, err2 := <-results, <-results
			if err := err1; err != nil {
				t.Fatalf("err1 error = %v", err)
			}
			if err := err2; err != nil {
				t.Fatalf("err2 error = %v", err)
			}
			if got, want := bb.Size(), int64(200); got != want {
				t.Errorf("bb.Size() = %#v, want %#v", got, want)
			}
			data, err := io.ReadAll(bb)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got, want := bytes.Count(data, []byte("a")), 100; got != want {
				t.Errorf("bytes.Count(data, []byte(\"a\")) = %#v, want %#v", got, want)
			}
			if got, want := bytes.Count(data, []byte("b")), 100; got != want {
				t.Errorf("bytes.Count(data, []byte(\"b\")) = %#v, want %#v", got, want)
			}
		})
	}
}

func TestBuffer_AlternatingWriteRead(t *testing.T) {
	bb := New()
	defer bb.Close()

	for i := 0; i < 10; i++ {
		// Write
		data := []byte{byte(i)}
		_, err := bb.Write(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Seek to beginning
	_, err := bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back
	for i := 0; i < 10; i++ {
		buf := make([]byte, 1)
		n, err := bb.Read(buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, want := n, 1; got != want {
			t.Fatalf("n = %#v, want %#v", got, want)
		}
		if buf[0] != byte(i) {
			t.Errorf("Read[%d] = %d, want %d", i, buf[0], i)
		}
	}
}

func TestBuffer_WriteTo_Empty(t *testing.T) {
	bb := New()
	defer bb.Close()

	var buf bytes.Buffer
	n, err := bb.WriteTo(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, int64(0); got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}
	if got, want := buf.Len(), 0; got != want {
		t.Fatalf("buf.Len() = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteTo_AfterPartialRead(t *testing.T) {
	bb := New()
	defer bb.Close()

	data := []byte("0123456789")
	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Seek to start and read 5 bytes
	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 5)
	_, err = bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// WriteTo should write remaining 5 bytes
	var outBuf bytes.Buffer
	n, err := bb.WriteTo(&outBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, int64(5); got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}
	if got, want := outBuf.String(), "56789"; got != want {
		t.Fatalf("outBuf.String() = %#v, want %#v", got, want)
	}
}

func TestBuffer_ReadFrom_Error(t *testing.T) {
	bb := New(WithMaxTotalBytes(10))
	defer bb.Close()

	// Try to read more than max
	r := strings.NewReader(strings.Repeat("a", 100))
	_, err := bb.ReadFrom(r)

	if err == nil {
		t.Errorf("err: expected an error")
	}
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
}

func TestBuffer_Size_AfterSeek(t *testing.T) {
	bb := New()
	defer bb.Close()

	data := []byte("test data")
	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	size1 := bb.Size()

	// Seek to middle
	_, err = bb.Seek(4, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	size2 := bb.Size()

	// Size should not change after seek
	if size1 != size2 {
		t.Errorf("Size changed after Seek: before=%d, after=%d", size1, size2)
	}
}

func TestBuffer_TransitionFromMemToFile(t *testing.T) {
	// Force transition by using small memory limit
	bb := New(WithMaxMemBytes(10))
	defer bb.Close()

	// Write small amount (should stay in memory)
	small := []byte("small")
	_, err := bb.Write(small)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write large amount (should transition to file)
	large := bytes.Repeat([]byte("a"), 100)
	_, err = bb.Write(large)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all data is readable
	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := append(small, large...)
	if !bytes.Equal(result, expected) {
		t.Error("Data corrupted during memory-to-file transition")
	}
}

func TestBuffer_BinaryData(t *testing.T) {
	bb := New()
	defer bb.Close()

	// Binary data with all byte values
	data := make([]byte, 256)
	for i := 0; i < 256; i++ {
		data[i] = byte(i)
	}

	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Equal(result, data) {
		t.Error("Binary data corrupted")
	}
}

func TestBuffer_SeekBeyondSize(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, err := bb.Write([]byte("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to seek way beyond
	_, err = bb.Seek(1000, io.SeekStart)

	// Behavior may vary - just ensure it doesn't crash
	t.Logf("Seek beyond size returned error: %v", err)
}

func TestBuffer_MaxMemBytes_Boundary(t *testing.T) {
	// Test exactly at boundary
	maxMem := 100
	bb := New(WithMaxMemBytes(maxMem))
	defer bb.Close()

	// Write exactly maxMemBytes
	data := bytes.Repeat([]byte("a"), maxMem)
	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write one more byte (should trigger file)
	_, err = bb.Write([]byte("b"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify data
	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(result), maxMem+1; got != want {
		t.Fatalf("len(result) = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteString(t *testing.T) {
	bb := New()
	defer bb.Close()

	text := "Hello, World!"
	n, err := bb.WriteString(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, len(text); got != want {
		t.Fatalf("n = %#v, want %#v", got, want)
	}

	_, err = bb.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(result), text; got != want {
		t.Fatalf("string(result) = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteString_AfterMaxSize(t *testing.T) {
	bb := New(WithMaxTotalBytes(5))
	defer bb.Close()

	_, err := bb.WriteString("Hello, World!")
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
}

// ---- Tests for previously uncovered code paths ----

func TestErrMaxSizeExceeded_Error(t *testing.T) {
	e := ErrMaxSizeExceeded(1024)
	if got, want := e.Error(), "diskbuffer: maximum size 1024 exceeded"; got != want {
		t.Errorf("e.Error() = %#v, want %#v", got, want)
	}
}

func TestBuffer_Limit(t *testing.T) {
	bb := New(WithMaxTotalBytes(42))
	defer bb.Close()
	if got, want := bb.(*buffer).Limit(), int64(42); got != want {
		t.Errorf("bb.(*buffer).Limit() = %#v, want %#v", got, want)
	}

	// Unlimited
	bb2 := New()
	defer bb2.Close()
	if got, want := bb2.(*buffer).Limit(), int64(unlimited); got != want {
		t.Errorf("bb2.(*buffer).Limit() = %#v, want %#v", got, want)
	}
}

func TestBuffer_Len(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, _ = bb.Write([]byte("0123456789"))
	if got, want := bb.(*buffer).Len(), int64(10); got != want {
		t.Errorf("bb.(*buffer).Len() = %#v, want %#v", got, want)
	}

	// After seeking forward, Len decreases
	_, _ = bb.Seek(3, io.SeekStart)
	if got, want := bb.(*buffer).Len(), int64(7); got != want {
		t.Errorf("bb.(*buffer).Len() = %#v, want %#v", got, want)
	}

	// After seeking to end, Len is 0
	_, _ = bb.Seek(0, io.SeekEnd)
	if got, want := bb.(*buffer).Len(), int64(0); got != want {
		t.Errorf("bb.(*buffer).Len() = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteByte(t *testing.T) {
	bb := New()
	defer bb.Close()

	for i := 0; i < 10; i++ {
		err := bb.WriteByte(byte('a' + i))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if got, want := bb.Size(), int64(10); got != want {
		t.Errorf("bb.Size() = %#v, want %#v", got, want)
	}

	_, _ = bb.Seek(0, io.SeekStart)
	data, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(data), "abcdefghij"; got != want {
		t.Errorf("string(data) = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteByte_ReadOnly(t *testing.T) {
	bb := New(WithReadOnly(true))
	defer bb.Close()
	err := bb.WriteByte('x')
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_WriteByte_MaxSize(t *testing.T) {
	// maxTotalBytes=0 means unlimited in New(), so use 1 to set a real limit
	bb := New(WithMaxMemBytes(1), WithMaxTotalBytes(1))
	defer bb.Close()
	// First byte goes into memory
	err := bb.WriteByte('a')
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Second should exceed total limit
	err = bb.WriteByte('b')
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
}

func TestBuffer_ReadByte_Sequential(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, _ = bb.Write([]byte("ABCDE"))
	_, _ = bb.Seek(0, io.SeekStart)

	for i, expected := range []byte("ABCDE") {
		c, err := bb.ReadByte()
		if err != nil {
			t.Fatalf("err error = %v: %s", err, fmt.Sprintf("ReadByte at position %d", i))
		}
		if got, want := c, expected; got != want {
			t.Errorf("c = %#v, want %#v", got, want)
		}
	}

	// Next read should return EOF
	_, err := bb.ReadByte()
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_ReadByte_AcrossBoundary(t *testing.T) {
	// Data spans memory and disk
	bb := New(WithMaxMemBytes(3))
	defer bb.Close()

	_, _ = bb.Write([]byte("ABCDE"))
	_, _ = bb.Seek(0, io.SeekStart)

	for i, expected := range []byte("ABCDE") {
		c, err := bb.ReadByte()
		if err != nil {
			t.Fatalf("err error = %v: %s", err, fmt.Sprintf("ReadByte at position %d", i))
		}
		if got, want := c, expected; got != want {
			t.Errorf("c = %#v, want %#v", got, want)
		}
	}
}

func TestBuffer_ReadByteAt_Memory(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("Hello"))

	c, err := bb.(*buffer).ReadByteAt(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := c, byte('H'); got != want {
		t.Errorf("c = %#v, want %#v", got, want)
	}

	c, err = bb.(*buffer).ReadByteAt(4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := c, byte('o'); got != want {
		t.Errorf("c = %#v, want %#v", got, want)
	}
}

func TestBuffer_ReadByteAt_File(t *testing.T) {
	bb := New(WithMaxMemBytes(3))
	defer bb.Close()
	_, _ = bb.Write([]byte("ABCDE"))

	// Byte at offset 4 is on disk
	c, err := bb.(*buffer).ReadByteAt(4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := c, byte('E'); got != want {
		t.Errorf("c = %#v, want %#v", got, want)
	}
}

func TestBuffer_ReadByteAt_OutOfRange(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("Hi"))

	_, err := bb.(*buffer).ReadByteAt(10)
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}

	_, err = bb.(*buffer).ReadByteAt(-1)
	if got, want := err, errInvalidOffset; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_ReadBytes_InMemory(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("line1\nline2\nline3"))
	_, _ = bb.Seek(0, io.SeekStart)

	line, err := bb.(*buffer).ReadBytes('\n')
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(line), "line1\n"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}

	line, err = bb.(*buffer).ReadBytes('\n')
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(line), "line2\n"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}

	// Third line has no trailing delimiter
	line, err = bb.(*buffer).ReadBytes('\n')
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
	if got, want := string(line), "line3"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}
}

func TestBuffer_ReadBytes_AcrossBoundary(t *testing.T) {
	// Memory holds 5 bytes, rest spills to disk
	bb := New(WithMaxMemBytes(5))
	defer bb.Close()

	_, _ = bb.Write([]byte("abc\ndefgh\nij"))
	_, _ = bb.Seek(0, io.SeekStart)

	// First line: "abc\n" — all in memory
	line, err := bb.(*buffer).ReadBytes('\n')
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(line), "abc\n"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}

	// Second line: "defgh\n" — starts in memory, delimiter is on disk
	line, err = bb.(*buffer).ReadBytes('\n')
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(line), "defgh\n"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}

	// Third: "ij" — all on disk, no delimiter
	line, err = bb.(*buffer).ReadBytes('\n')
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
	if got, want := string(line), "ij"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}
}

func TestBuffer_ReadBytes_Empty(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, _ = bb.Seek(0, io.SeekStart)
	_, err := bb.(*buffer).ReadBytes('\n')
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_ReadBytes_OnlyOnDisk(t *testing.T) {
	// maxMem=0 forces immediate file spill
	bb := New(WithMaxMemBytes(0))
	defer bb.Close()

	_, _ = bb.Write([]byte("aa\nbb"))
	_, _ = bb.Seek(0, io.SeekStart)

	line, err := bb.(*buffer).ReadBytes('\n')
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(line), "aa\n"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}

	line, err = bb.(*buffer).ReadBytes('\n')
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
	if got, want := string(line), "bb"; got != want {
		t.Errorf("string(line) = %#v, want %#v", got, want)
	}
}

func TestBuffer_Peek_Negative(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("data"))
	_, _ = bb.Seek(0, io.SeekStart)

	_, err := bb.(*buffer).Peek(-1)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

func TestBuffer_Peek_Zero(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("data"))
	_, _ = bb.Seek(0, io.SeekStart)

	p, err := bb.(*buffer).Peek(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := p; len(got) != 0 {
		t.Errorf("p = %v, want empty", got)
	}
}

func TestBuffer_Peek_ExceedsLen(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("hi"))
	_, _ = bb.Seek(0, io.SeekStart)

	p, err := bb.(*buffer).Peek(100)
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
	if got, want := string(p), "hi"; got != want {
		t.Errorf("string(p) = %#v, want %#v", got, want)
	}
}

func TestBuffer_Peek_AtEnd(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("data"))
	_, _ = bb.Seek(0, io.SeekEnd)

	p, err := bb.(*buffer).Peek(1)
	if got, want := err, io.EOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
	if got := p; len(got) != 0 {
		t.Errorf("p = %v, want empty", got)
	}
}

func TestBuffer_Peek_AcrossBoundary(t *testing.T) {
	bb := New(WithMaxMemBytes(3))
	defer bb.Close()
	_, _ = bb.Write([]byte("ABCDE"))
	_, _ = bb.Seek(1, io.SeekStart)

	// Peek 4 bytes: B(mem) C(mem) D(file) E(file)
	p, err := bb.(*buffer).Peek(4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(p), "BCDE"; got != want {
		t.Errorf("string(p) = %#v, want %#v", got, want)
	}

	// Position should not have changed
	if got, want := bb.(*buffer).pos, int64(1); got != want {
		t.Errorf("bb.(*buffer).pos = %#v, want %#v", got, want)
	}
}

func TestBuffer_Seek_InvalidWhence(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, err := bb.Seek(0, 99)
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got, want := err.Error(), "invalid whence"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func TestBuffer_ReadAt_NegativeOffset(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("test"))

	buf := make([]byte, 4)
	_, err := bb.ReadAt(buf, -1)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

func TestBuffer_ReadAt_EmptyBuffer(t *testing.T) {
	bb := New()
	defer bb.Close()

	// Empty read should succeed
	buf := make([]byte, 0)
	n, err := bb.ReadAt(buf, 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := n, 0; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteString_SpillsToDisk(t *testing.T) {
	bb := New(WithMaxMemBytes(5))
	defer bb.Close()

	n, err := bb.(*buffer).WriteString("Hello, World!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 13; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}

	_, _ = bb.Seek(0, io.SeekStart)
	data, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(data), "Hello, World!"; got != want {
		t.Errorf("string(data) = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteString_ReadOnly(t *testing.T) {
	bb := New(WithReadOnly(true))
	defer bb.Close()

	_, err := bb.WriteString("test")
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_WriteString_Empty(t *testing.T) {
	bb := New()
	defer bb.Close()

	n, err := bb.WriteString("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 0; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
}

func TestBuffer_ReadFrom_ReadError(t *testing.T) {
	bb := New()
	defer bb.Close()

	r := &errReader{err: io.ErrUnexpectedEOF}
	_, err := bb.ReadFrom(r)
	if got, want := err, io.ErrUnexpectedEOF; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

type errReader struct{ err error }

func (r *errReader) Read(p []byte) (int, error) {
	return 0, r.err
}

func TestBuffer_ReadFrom_ReadOnly(t *testing.T) {
	bb := New(WithReadOnly(true))
	defer bb.Close()

	_, err := bb.ReadFrom(strings.NewReader("data"))
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_WriteTo_WithFileBuffer(t *testing.T) {
	bb := New(WithMaxMemBytes(3))
	defer bb.Close()

	_, _ = bb.Write([]byte("ABCDE"))
	_, _ = bb.Seek(0, io.SeekStart)

	var out bytes.Buffer
	n, err := bb.WriteTo(&out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, int64(5); got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := out.String(), "ABCDE"; got != want {
		t.Errorf("out.String() = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteTo_FromMiddle(t *testing.T) {
	// Start reading from a position that's inside the file portion
	bb := New(WithMaxMemBytes(3))
	defer bb.Close()

	_, _ = bb.Write([]byte("ABCDE"))
	_, _ = bb.Seek(4, io.SeekStart) // position is in file portion

	var out bytes.Buffer
	n, err := bb.WriteTo(&out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, int64(1); got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := out.String(), "E"; got != want {
		t.Errorf("out.String() = %#v, want %#v", got, want)
	}
}

func TestBuffer_Write_ReadOnly(t *testing.T) {
	bb := New(WithReadOnly(true))
	defer bb.Close()

	_, err := bb.Write([]byte("test"))
	if got, want := err, ErrReadOnly; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

func TestBuffer_Options_WithTmpDir(t *testing.T) {
	tmpDir := t.TempDir()
	bb := New(WithMaxMemBytes(0), WithTmpDir(tmpDir))
	defer bb.Close()

	_, err := bb.Write([]byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = bb.Seek(0, io.SeekStart)
	data, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(data), "data"; got != want {
		t.Errorf("string(data) = %#v, want %#v", got, want)
	}
}

func TestBuffer_Options_WithMemBufferSizeHint(t *testing.T) {
	bb := New(WithMemBufferSizeHint(64))
	defer bb.Close()

	_, err := bb.Write([]byte("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := bb.Size(), int64(4); got != want {
		t.Errorf("bb.Size() = %#v, want %#v", got, want)
	}
}

func TestBuffer_Options_MemExceedsTotal(t *testing.T) {
	// maxMemBytes larger than maxTotalBytes should be clamped
	bb := New(WithMaxMemBytes(1000), WithMaxTotalBytes(10))
	defer bb.Close()

	// Write exactly 10 bytes (total limit)
	data := []byte("0123456789")
	_, err := bb.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Writing one more should fail
	_, err = bb.Write([]byte("x"))
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
}

func TestBuffer_Write_SpillCreatesFileBuffer(t *testing.T) {
	bb := New(WithMaxMemBytes(5), WithMaxTotalBytes(20))
	defer bb.Close()

	// Write 5 bytes to fill memory
	_, err := bb.Write([]byte("12345"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bb.(*buffer).fileBuf; got != nil {
		t.Errorf("bb.(*buffer).fileBuf = %v, want nil", got)
	}

	// Write 1 more byte to trigger file creation
	_, err = bb.Write([]byte("6"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bb.(*buffer).fileBuf; got == nil {
		t.Errorf("bb.(*buffer).fileBuf = %v, want non-nil", got)
	}

	// Read back all data
	_, _ = bb.Seek(0, io.SeekStart)
	data, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(data), "123456"; got != want {
		t.Errorf("string(data) = %#v, want %#v", got, want)
	}
}

func TestBuffer_Write_NoSpaceLeft(t *testing.T) {
	// maxMem=3, maxTotal=3 → no file buffer allowed
	bb := New(WithMaxMemBytes(3), WithMaxTotalBytes(3))
	defer bb.Close()

	_, err := bb.Write([]byte("abc"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n, err := bb.Write([]byte("d"))
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
	if got, want := n, 0; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteString_NoSpaceLeft(t *testing.T) {
	bb := New(WithMaxMemBytes(3), WithMaxTotalBytes(3))
	defer bb.Close()

	_, err := bb.WriteString("abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = bb.WriteString("d")
	if _, ok := err.(ErrMaxSizeExceeded); !ok {
		t.Errorf("error type = %T, want ErrMaxSizeExceeded", err)
	}
}

func TestBuffer_ReadFrom_SpillsToDisk(t *testing.T) {
	bb := New(WithMaxMemBytes(5))
	defer bb.Close()

	r := strings.NewReader("hello world!!")
	n, err := bb.ReadFrom(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, int64(13); got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}

	_, _ = bb.Seek(0, io.SeekStart)
	data, err := io.ReadAll(bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(data), "hello world!!"; got != want {
		t.Errorf("string(data) = %#v, want %#v", got, want)
	}
}

func TestBuffer_ReadAt_AcrossBoundary(t *testing.T) {
	bb := New(WithMaxMemBytes(4))
	defer bb.Close()

	_, _ = bb.Write([]byte("ABCDEFGH"))

	buf := make([]byte, 6)
	n, err := bb.ReadAt(buf, 1) // B C D(mem) E F G(file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 6; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := string(buf), "BCDEFG"; got != want {
		t.Errorf("string(buf) = %#v, want %#v", got, want)
	}
}

func TestBuffer_Read_AdvancesPosition(t *testing.T) {
	bb := New()
	defer bb.Close()

	_, _ = bb.Write([]byte("ABCDE"))
	_, _ = bb.Seek(0, io.SeekStart)

	buf := make([]byte, 2)
	n, err := bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 2; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := string(buf), "AB"; got != want {
		t.Errorf("string(buf) = %#v, want %#v", got, want)
	}

	n, err = bb.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := n, 2; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := string(buf), "CD"; got != want {
		t.Errorf("string(buf) = %#v, want %#v", got, want)
	}

	buf = make([]byte, 5)
	n, err = bb.Read(buf)
	if got, want := n, 1; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := buf[0], byte('E'); got != want {
		t.Errorf("buf[0] = %#v, want %#v", got, want)
	}
}

func TestBuffer_WriteTo_WriterError(t *testing.T) {
	bb := New()
	defer bb.Close()
	_, _ = bb.Write([]byte("test data"))
	_, _ = bb.Seek(0, io.SeekStart)

	w := &failWriter{}
	_, err := bb.WriteTo(w)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

type failWriter struct{}

func (w *failWriter) Write(p []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestBuffer_WriteTo_FileWriterError(t *testing.T) {
	bb := New(WithMaxMemBytes(3))
	defer bb.Close()
	_, _ = bb.Write([]byte("ABCDE"))
	_, _ = bb.Seek(0, io.SeekStart)

	// Read past memory portion first
	buf := make([]byte, 3)
	_, _ = bb.Read(buf)

	// Now WriteTo writes from file, which should fail
	w := &failWriter{}
	_, err := bb.WriteTo(w)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}
