package pdf2txt

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/EndFirstCorp/pdflib/bufio"
	"github.com/pkg/errors"
)

var (
	errUnexpectedEOF = errors.New("Unexpected end of file")
)

type pdfDocument struct {
	items []interface{}
}

type pdfDictionary struct {
	Entries map[name]interface{}
}

type token string
type name string

func (d *pdfDictionary) Length() int {
	for key, value := range d.Entries {
		if strings.HasSuffix(string(key), "/Length") {
			if data, ok := value.(token); ok && len(data) > 0 {
				length, err := strconv.Atoi(string(data))
				if err != nil {
					return 0
				}
				return length
			}
			return 0
		}
	}
	return 0
}

const endLine = 0

var spaceChars = []byte{'\x00', '\t', '\f', ' ', '\n', '\r'}
var eolChars = []byte{'\r', '\n'}
var delimChars = append(spaceChars, '(', ')', '<', '>', '[', ']', '{', '}', '/', '%')

// parse reads through the entire PDF document and places the entire document
// structure into memory. The assumption made here is that holding all of the
// text content in memory isn't too much to handle
func parsePdf(pdf io.Reader) (*pdfDocument, error) {
	br := bufio.NewReader(pdf)
	var b byte
	var err error
	d := &pdfDocument{}
	count := 0
	for b, err = br.ReadByte(); err == nil; b, err = br.ReadByte() {
		item := readNext(b, br)
		fmt.Printf("%d - %T %v\n", count, item, item)
		if err, ok := item.(error); ok {
			return d, err
		}
		d.items = append(d.items, item)
		if tok, ok := item.(token); ok {
			if tok == "stream" {
				if dict, ok := d.items[len(d.items)-2].(*pdfDictionary); ok {
					if l := dict.Length(); l > 0 {
						s, err := readStream(br, pdf, l)
						if err != nil {
							return nil, err
						}
						d.items = append(d.items, s)
					}
				}
			}
		}
		if count == 3000 {
			return d, err
		}
		count++
	}
	return d, err
}

func readNext(b byte, r *bufio.Reader) interface{} {
	switch b {
	case '(':
		v, err := readUntil(r, ')') // make into readText
		if err != nil {
			return err
		}
		return string(v)
	case '<':
		n, err := r.Peek(1)
		if err != nil {
			return err
		}
		if n[0] == '<' {
			r.ReadByte() // read the "<" we peeked
			d, err := readDictionary(r)
			if err != nil {
				return err
			}
			return d
		}

		v, err := readUntil(r, '>')
		if err != nil {
			return err
		}
		return string(v)
	case '[':
		v, err := readArray(r)
		if err != nil {
			return err
		}
		return v
	case '{':
		v, err := readUntil(r, '}') // possibly make into readCodeStream (even though we're not using data)
		if err != nil {
			return err
		}
		return v
	// the only way we should've gotten here is from inside a dictionary calling readNext when there is no value
	// this is known as the nullValue for pdfs
	case ')', '>', ']', '}':
		r.UnreadByte() // get back the end character
		return nil
	case '/':
		r.UnreadByte() // want the starting slash too
		v, err := readName(r)
		if err != nil {
			return nil
		}
		return v
	case '%':
		v, _, err := readUntilAny(r, eolChars) // make into readComment
		if err != nil {
			return err
		}
		return string(v)
	case '\x00', '\t', '\f', ' ', '\n', '\r':
		err := skipSpaces(r)
		next, err := r.ReadByte()
		if err != nil {
			return err
		}
		return readNext(next, r)
	default:
		err := r.UnreadByte() // include first character in token
		token, err := readToken(r)
		if err != nil {
			return err
		}
		return token
	}
}

func isEOL(b byte) bool {
	return b == '\n' || b == '\r'
}

func isWhitespace(b byte) bool {
	return b == '\x00' || b == '\t' || b == '\f' || b == ' ' || isEOL(b)
}

func isDelimiter(b byte) bool {
	return b == '(' || b == ')' || b == '<' || b == '>' ||
		b == '[' || b == ']' || b == '{' || b == '}' || b == '/' || b == '%'
}

func isRegular(b byte) bool {
	return !isWhitespace(b) && !isDelimiter(b)
}

func readStream(br *bufio.Reader, r io.Reader, length int) ([]byte, error) {
	fmt.Println("reading stream of length", length)
	var err error
	s := make([]byte, length)
	bSize := br.Buffered()
	if bSize < length { // pull directly from reader since buffer is too small
		buf := make([]byte, bSize) // get rest of buffer
		_, err = br.Read(buf)
		if err != nil {
			return nil, err
		}

		pull := make([]byte, length-bSize) // pull what isn't buffered
		_, err = r.Read(pull)
		copy(s[:bSize], buf)
		copy(s[bSize:], pull)
		br.Reset(r) // reset buffered reader state
	} else {
		_, err = br.Read(s)
	}

	if err != nil {
		return nil, err
	}
	return s, nil
}

func readArray(r *bufio.Reader) ([]interface{}, error) {
	items := []interface{}{}
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == ']' {
			return items, nil
		}
		item := readNext(b, r)
		if err, ok := item.(error); ok {
			return nil, errors.Wrap(err, fmt.Sprintf("error while reading array item"))
		}
		items = append(items, item)
	}
}

func readName(r *bufio.Reader) (name, error) {
	endChars := append(spaceChars, '(', ')', '<', '>', '[', ']', '{', '}', '%') // any except /
	v, end, err := readUntilAny(r, endChars)
	if err != nil {
		return "", err
	}
	if isDelimiter(end) {
		r.UnreadByte() // back out the delimiter
	}
	return name(v), err
}

func readDictionary(r *bufio.Reader) (*pdfDictionary, error) {
	d := &pdfDictionary{Entries: make(map[name]interface{})}
	for {
		name, err := readName(r)
		if err != nil {
			return nil, err
		}
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		item := readNext(b, r)
		if err, ok := item.(error); ok {
			return nil, errors.Wrap(err, fmt.Sprintf("error while reading from %s", name))
		}
		d.Entries[name] = item

		err = skipSpaces(r)
		if err != nil {
			return nil, err
		}
		p, err := r.Peek(2)
		if err != nil {
			return d, err
		}
		if string(p) == ">>" {
			r.Read(p) // move forward read pointer
			return d, nil
		}
	}
}

func readToken(r *bufio.Reader) (token, error) {
	tok, next, err := readUntilAny(r, delimChars)
	if err != nil {
		return "\x00", err
	}

	if string(tok) == "stream" { // special case for stream
		if next == '\r' { // EOL is \r, so take \n as well (section 3.2.7)
			next, err := r.ReadByte()
			if err != nil {
				return "\x00", err
			}
			if next != '\n' { // doesn't follow spec
				return "\x00", errors.New("expected \r\n EOL delimiter")
			}
		}
	}

	if isDelimiter(next) {
		r.UnreadByte() // back out delimiter
	}
	return token(tok), nil
}

func readUntil(r *bufio.Reader, endAt byte) ([]byte, error) {
	v, _, err := readUntilAny(r, []byte{endAt})
	return v, err
}

func readUntilAny(r *bufio.Reader, endAtAny []byte) ([]byte, byte, error) {
	var result []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return result, '\x00', err
		}
		for i := range endAtAny {
			if b == endAtAny[i] {
				return result, b, err
			}
		}
		result = append(result, b)
	}
}

func skipSpaces(r *bufio.Reader) error {
	_, err := skipSubsequent(r, spaceChars)
	return err
}

// skipSubsequent skips all consecutive characters in any order
func skipSubsequent(r *bufio.Reader, skip []byte) (bool, error) {
	var found bool
	for {
		b, err := r.Peek(1) // check next byte
		if err != nil {
			return found, err
		}
		nextb := b[0]
		for i := range skip {
			if nextb == skip[i] { // found match, so do actual read to skip
				found = true
				_, err = r.ReadByte()
				if err != nil {
					return found, err
				}
				break
			}
		}
		return found, nil
	}
}

func getToken(r *bufio.Reader) ([]byte, error) {
	return nil, nil
}
