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
	Entries map[string][]byte
}

type pdfStream struct {
}

type pdfNull struct {
}

type pdfDocument struct {
}

const endLine = 0

var spaceChars = []byte{'\x00', '\t', '\f', ' ', '\n', '\r'}
var eolChars = []byte{'\r', '\n'}

// parse reads through the entire PDF document and places the entire document
// structure into memory. The assumption made here is that holding all of the
// text content in memory isn't too much to handle
func parsePdf(pdf io.Reader) (*pdfDocument, error) {
	br := bufio.NewReader(pdf)
	var b byte
	var err error
	for b, err = br.ReadByte(); err == nil; b, err = br.ReadByte() {
		readObjects(b, br)
	}
	return &pdfDocument{}, err
}

func readObjects(b byte, r *bufio.Reader) error {
	var err error
	var v []byte
	switch b {
	case '(':
		v, err = readUntil(r, ')') // make into readText
		fmt.Println("string", string(v), err)
	case '<':
		n, err := r.Peek(1)
		if err != nil {
			return err
		}
		if n[0] == '<' {
			var d *pdfDictionary
			d, err = readDictionary(r)
			fmt.Println("dictionary", d)
		}
	case '[':
		v, err = readUntil(r, ']') // make into readArray
		fmt.Println("array", string(v), err)
	case '{':
		v, err = readUntil(r, '}') // possibly make into readCodeStream (even though we're not using data)
		fmt.Println("codestream", string(v), err)
	case '/':
		v, _, err = readName(r)
		fmt.Println("name", string(v), err)
	case '%':
		v, _, err = readUntilAny(r, eolChars) // make into readComment
		fmt.Println("comment", string(v), err)
	case '\x00', '\t', '\f', ' ', '\n', '\r':
		_, err = skipSubsequent(r, spaceChars)
	default:
		v, _, err = readUntilAny(r, spaceChars) // make into get token
		token := append([]byte{b}, v...)        // include the byte we already pulled
		fmt.Println("token", string(token), err)
	}
	return err
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
	return v, end, nil
}

func readDictionary(r *bufio.Reader) (*pdfDictionary, error) {
	d := &pdfDictionary{}
	for {
		name, b, err := readName(r)
		if err != nil {
			return nil, err
		}
		err = readObjects(b, r)
		b, err := r.Peek(2)
		if err != nil {

		}
	}
	return d, err
}

func readUntil(r *bufio.Reader, endAt byte) ([]byte, error) {
	v, b, err := readUntilAny(r, []byte{endAt})
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
