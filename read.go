package pdf2txt

import (
	"bufio"
	"errors"
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
	s := make([]byte, size)
	actual := b.br.Buffered()
	if actual < size { // pull directly from reader since buffer is too small
		buf := make([]byte, actual) // get rest of buffer
		l, err := b.br.Read(buf)
		if err != nil {
			return nil, err
		}
		if l != actual {
			return nil, errors.New("couldn't get all of remaining buffer")
		}
		copy(s[:actual], buf)

		for actual != size { // may need to read more than once to get full amount
			pull := make([]byte, size-actual) // pull what is left
			pSize, err := b.r.Read(pull)
			if err != nil {
				return nil, err
			}
			copy(s[actual:], pull)
			actual += pSize // bytes read from underlying reader + buffered bytes
		}
		b.br.Reset(b.r) // reset buffered reader state
		return s, nil
	}
	_, err := b.br.Read(s)
	return s, err // only return the actual valid number of bytes
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
