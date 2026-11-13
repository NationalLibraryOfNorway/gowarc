package diskbuffer

import (
	"testing"
)

func TestMemBuffer_SizeAndLimit(t *testing.T) {
	m := newMemBuffer(100, 16)
	if got, want := m.Size(), int64(0); got != want {
		t.Errorf("m.Size() = %#v, want %#v", got, want)
	}
	if got, want := m.Limit(), int64(100); got != want {
		t.Errorf("m.Limit() = %#v, want %#v", got, want)
	}
	if got := m.hasSpace(); !got {
		t.Errorf("m.hasSpace() = %v, want true", got)
	}

	m.write([]byte("hello"))
	if got, want := m.Size(), int64(5); got != want {
		t.Errorf("m.Size() = %#v, want %#v", got, want)
	}
	if got, want := string(m.bytes()), "hello"; got != want {
		t.Errorf("string(m.bytes()) = %#v, want %#v", got, want)
	}
}

func TestMemBuffer_Full(t *testing.T) {
	m := newMemBuffer(5, 5)
	m.write([]byte("12345"))
	if got := m.hasSpace(); got {
		t.Errorf("m.hasSpace() = %v, want false", got)
	}

	// Writing more returns 0
	n := m.write([]byte("x"))
	if got, want := n, 0; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
}

func TestMemBuffer_WriteString(t *testing.T) {
	m := newMemBuffer(10, 4)
	n := m.writeString("hello")
	if got, want := n, 5; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
	if got, want := string(m.bytes()), "hello"; got != want {
		t.Errorf("string(m.bytes()) = %#v, want %#v", got, want)
	}

	// Write more than free space — should be truncated
	n = m.writeString("world!")
	if got, want := n, 5; got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	} // only 5 bytes free
	if got, want := string(m.bytes()), "helloworld"; got != want {
		t.Errorf("string(m.bytes()) = %#v, want %#v", got, want)
	}
}

func TestMemBuffer_NewWithEdgeCases(t *testing.T) {
	// Negative limit → 0
	m := newMemBuffer(-1, 0)
	if got, want := m.Limit(), int64(0); got != want {
		t.Errorf("m.Limit() = %#v, want %#v", got, want)
	}

	// Negative initial cap
	m = newMemBuffer(100, -1)
	if got, want := m.Limit(), int64(100); got != want {
		t.Errorf("m.Limit() = %#v, want %#v", got, want)
	}

	// Hint > limit → capped to limit
	m = newMemBuffer(10, 20)
	if got, want := m.Limit(), int64(10); got != want {
		t.Errorf("m.Limit() = %#v, want %#v", got, want)
	}
	if got, bound := cap(m.bytes()), 10; got > bound {
		t.Errorf("cap(m.bytes()) = %v, want <= %v", got, bound)
	}
}
