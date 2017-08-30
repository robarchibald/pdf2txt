package pdf2txt

import (
	"bufio"
	"io"
)

type peekingReader interface {
	Peek(n int) ([]byte, error)
	ReadByte() (byte, error)
	ReadBytes(size int) ([]byte, error)
}

type memReader struct {
	buf []byte
	i   int
}

type bufReader struct {
	r  io.Reader
	br *bufio.Reader
}

func newMemReader(b []byte) *memReader {
	return &memReader{b, 0}
}

func (b *memReader) Peek(n int) ([]byte, error) {
	if b.i+n > len(b.buf) {
		return nil, io.EOF
	}
	return b.buf[b.i : b.i+n], nil
}

func (b *memReader) ReadByte() (byte, error) {
	if b.i+1 > len(b.buf) {
		return 0, io.EOF
	}
	v := b.buf[b.i]
	b.i++
	return v, nil
}

func (b *memReader) ReadBytes(size int) ([]byte, error) {
	if b.i+size > len(b.buf) {
		return nil, io.EOF
	}
	v := b.buf[b.i : b.i+size]
	b.i += size
	return v, nil
}

func newBufReader(r io.Reader) *bufReader {
	return &bufReader{r, bufio.NewReader(r)}
}

func (b *bufReader) Peek(n int) ([]byte, error) {
	return b.br.Peek(n)
}

func (b *bufReader) ReadByte() (byte, error) {
	return b.br.ReadByte()
}

func (b *bufReader) ReadBytes(size int) ([]byte, error) {
	var err error
	s := make([]byte, size)
	bSize := b.br.Buffered()
	if bSize < size { // pull directly from reader since buffer is too small
		buf := make([]byte, bSize) // get rest of buffer
		_, err = b.br.Read(buf)
		if err != nil {
			return nil, err
		}

		pull := make([]byte, size-bSize) // pull what isn't buffered
		_, err = b.r.Read(pull)
		copy(s[:bSize], buf)
		copy(s[bSize:], pull)
		b.br.Reset(b.r) // reset buffered reader state
	} else {
		_, err = b.br.Read(s)
	}
	return s, nil
}

func readUntil(r peekingReader, endAt byte) ([]byte, error) {
	return readUntilAny(r, []byte{endAt})
}

func readUntilAny(r peekingReader, endAtAny []byte) ([]byte, error) {
	var result []byte
	for {
		p, err := r.Peek(1)
		if err != nil {
			return result, err
		}
		for i := range endAtAny {
			if p[0] == endAtAny[i] {
				return result, err
			}
		}
		r.ReadByte() // move read pointer forward since we used this byte
		result = append(result, p[0])
	}
}

func skipSpaces(r peekingReader) error {
	_, err := skipSubsequent(r, spaceChars)
	return err
}

// skipSubsequent skips all consecutive characters in any order
func skipSubsequent(r peekingReader, skip []byte) (bool, error) {
	var found bool
	for {
		b, err := r.Peek(1) // check next byte
		if err != nil {
			return found, err
		}
		next := b[0]
		for i := range skip {
			if next == skip[i] { // found match, so do actual read to skip
				found = true
				r.ReadByte() // move read pointer forward since we used this byte
				break
			}
		}
		return found, nil
	}
}
