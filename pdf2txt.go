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

type pdfBool struct {
}

type pdfNumber struct {
}

type pdfString struct {
}

type pdfName struct {
}

type pdfArray struct {
}

type pdfDictionary struct {
	Entries map[string]pdfItem
}

func (d *pdfDictionary) Length() int {
	for key, value := range d.Entries {
		if strings.HasSuffix(key, "/Length") {
			if data, ok := value.([]string); ok && len(data) > 0 {
				length, err := strconv.Atoi(data[0])
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

type pdfStream struct {
}

type pdfNull struct {
}

type pdfItem interface{}

type pdfDocument struct {
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
	for b, err = br.ReadByte(); err == nil; b, err = br.ReadByte() {
		item := readNext(b, br)
		fmt.Println(item)
		if err, ok := item.(error); ok {
			return d, err
		}
	}
	return d, err
}

func readNext(b byte, r *bufio.Reader) pdfItem {
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
			r.ReadByte()
			d, err := readDictionary(r)
			if err != nil {
				return err
			}
			if l := d.Length(); l != 0 {
				fmt.Println("has a length", l)
				return errors.New("quitting. Try to read stream")
			}
		}

		v, err := readUntil(r, '>')
		if err != nil {
			return err
		}
		return string(v)
	case '[':
		v, err := readUntil(r, ']') // make into readArray
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
	case '/':
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
		tokens, err := readTokens(r)
		if err != nil {
			return err
		}
		return tokens
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

func readName(r *bufio.Reader) ([]byte, error) {
	endChars := append(spaceChars, '(', ')', '<', '>', '[', ']', '{', '}', '%') // any except /
	v, end, err := readUntilAny(r, endChars)
	if err != nil {
		return nil, err
	}
	if isDelimiter(end) {
		r.UnreadByte() // back out the delimiter
	}
	if len(v) > 0 && v[0] != '/' { // start with '/ no matter what
		v = append([]byte{'/'}, v...)
	}
	return v, err
}

func readDictionary(r *bufio.Reader) (*pdfDictionary, error) {
	d := &pdfDictionary{Entries: make(map[string]pdfItem)}
	for {
		name, err := readName(r)
		if err != nil {
			return nil, err
		}
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		d.Entries[string(name)] = readNext(b, r)
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

func readTokens(r *bufio.Reader) ([]string, error) {
	tokens := []string{}
	r.UnreadByte() // want to include first byte in token
	for {
		tok, next, err := readUntilAny(r, delimChars)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, string(tok))

		if isWhitespace(next) {
			err = skipSpaces(r)
			if err != nil {
				return nil, err
			}
		} else if isDelimiter(next) {
			r.UnreadByte() // back out delimiter
			break
		}

		// quit if we've run out of regular tokens
		p, err := r.Peek(1)
		if err != nil {
			return nil, err
		}
		if !isRegular(p[0]) {
			break
		}
	}
	return tokens, nil
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
