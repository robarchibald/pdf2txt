package pdf2txt

import (
	"fmt"
	"io"

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

type pdfStream struct {
}

type pdfNull struct {
}

type endDictionary struct{}
type endArray struct{}

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
		readNext(b, br)
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
		fmt.Println("string", string(v))
		return string(v)
	case '<':
		n, err := r.Peek(1)
		if err != nil {
			return err
		}
		if n[0] == '<' {
			r.ReadByte()
			var d *pdfDictionary
			d, err = readDictionary(r)
			if err != nil {
				return err
			}
			fmt.Println("dictionary", d)
			return d
		}

		v, err := readUntil(r, '>')
		if err != nil {
			return err
		}
		fmt.Println("array", string(v))
		return v
	case '[':
		v, err := readUntil(r, ']') // make into readArray
		if err != nil {
			return err
		}
		fmt.Println("array", string(v))
		return v
	case '{':
		v, err := readUntil(r, '}') // possibly make into readCodeStream (even though we're not using data)
		if err != nil {
			return err
		}
		fmt.Println("codestream", string(v))
		return v
	case '/':
		v, _, err := readName(r)
		fmt.Println("name", string(v), err)
	case '%':
		v, _, err := readUntilAny(r, eolChars) // make into readComment
		fmt.Println("comment", string(v), err)
	case '\x00', '\t', '\f', ' ', '\n', '\r':
		err := skipSpaces(r)
		next, err := r.ReadByte()
		if err != nil {
			return err
		}
		return readNext(next, r)
	default:
		tokens := []string{}
		r.UnreadByte() // want to include first byte in token
		for {
			tok, next, err := readUntilAny(r, delimChars)
			if err != nil {
				break
			}
			tokens = append(tokens, string(tok))

			if isWhitespace(next) {
				err = skipSpaces(r)
				if err != nil {
					break
				}
			} else if isDelimiter(next) {
				break
			}

			// quit if we've run out of regular tokens
			p, err := r.Peek(1)
			if err != nil {
				return err
			}
			if !isRegular(p[0]) {
				break
			}
		}
		fmt.Println("tokens", tokens)
		return tokens
	}
	return nil
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

func readName(r *bufio.Reader) ([]byte, byte, error) {
	endChars := append(spaceChars, '[', '(', '<', '%')
	v, end, err := readUntilAny(r, endChars)
	if err != nil {
		return nil, end, err
	}
	if len(v) > 0 && v[0] != '/' { // start with '/ no matter what
		v = append([]byte{'/'}, v...)
	}
	fmt.Println("name", string(v), err)
	return v, end, nil
}

func readDictionary(r *bufio.Reader) (*pdfDictionary, error) {
	d := &pdfDictionary{Entries: make(map[string]pdfItem)}
	for {
		name, b, err := readName(r)
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
