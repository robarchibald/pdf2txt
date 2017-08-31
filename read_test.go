package pdf2txt

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestMemReadBytes(t *testing.T) {
	r := newMemReader([]byte("hello"))
	b, _ := r.ReadBytes(3)
	if string(b) != "hel" {
		t.Error("expected correct bytes")
	}
	if _, err := r.ReadBytes(3); err != io.EOF {
		t.Error("expected EOF", err)
	}
}
func TestBufReadBytes(t *testing.T) {
	r := newBufReader(strings.NewReader("hello there my friend. How are you?"))
	b, _ := r.ReadBytes(31)
	if string(b) != "hello there my friend. How are " {
		t.Error("expected correct bytes", string(b))
	}
	if b, err := r.ReadBytes(5); err != nil || len(b) != 4 || string(b) != "you?" {
		t.Error("expected shorter string", err, string(b))
	}
	if _, err := r.ReadByte(); err == nil {
		t.Fatal("expected error", err)
	}
}

type erroringPeeker struct{}

func (p *erroringPeeker) Peek(n int) ([]byte, error) {
	return nil, errors.New("fail")
}

func (p erroringPeeker) ReadByte() (byte, error) {
	return '\x00', nil
}

func (p erroringPeeker) ReadBytes(size int) ([]byte, error) {
	return nil, nil
}
