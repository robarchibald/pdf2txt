package pdf2txt

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"strconv"

	"github.com/pkg/errors"
)

type comment string
type dictionary map[name]interface{}
type stream []byte
type text string
type array []interface{}
type hexdata string
type name string
type codestream string
type token string
type null bool
type end byte
type xref map[string]xrefItem
type cmap map[hexdata]string
type rootnode string
type objectref struct {
	refString string
	refType   string
}
type object struct {
	refString string
	values    []interface{}
	dict      dictionary
	stream    io.Reader
	isObjStm  bool
	n         int
	first     int
}
type xrefItem struct {
	byteOffset int
	xrefType   string
}
type textsection struct {
	fontName  name
	textArray array
	text      text
}

var spaceChars = []byte{'\x00', '\t', '\f', ' ', '\n', '\r'}
var eolChars = []byte{'\r', '\n'}
var delimChars = append(spaceChars, '(', ')', '<', '>', '[', ']', '{', '}', '/', '%')

// Tokenize reads through the entire PDF document and adds a token
// to the tChan every time it encounters a token making it possible to process
// tokens in parallel
//
// Types of tokens supported:
//   - comment       : from % to end of line (\r or \n)
//   - dictionary    : from << to >>
//   - stream        : uses length from dictionary. Data is from stream to endstream
//   - text          : from ( to )
//   - array         : from [ to ]
//   - hexdata       : from < to >
//   - name          : from / to space, EOL or other delimiter
//   - codestream    : from { to }
//   - token         : any other text delimited by space, EOL or other delimiter
//   - object        : from "x x obj" to endobj (e.g. 250 0 obj is the 250th object)
//   - xref          : from xref to however many records are needed
//   - objectref     : three subsequent tokens "x x R" or "x x obj" (e.g. 250 0 obj)
//   - textsection   : from BT to ET
//   - cmap          : from begincmap to endcmap
func Tokenize(r peekingReader, tChan chan interface{}) {
	var err error

Loop:
	for {
		item := readNext(r)

		switch v := item.(type) {
		case error:
			err = v
			break Loop

		case *objectref:
			if v.refType == "obj" {
				var obj *object
				obj, err = readObject(r, v)
				if err != nil {
					break Loop
				}
				tChan <- obj
			}

		case token:
			switch v {
			case "xref":
				var xref xref
				xref, err = readXref(r)
				if err != nil {
					break Loop
				}
				tChan <- xref

			case "trailer":
				var trailer rootnode
				trailer, err = readTrailer(r)
				if err != nil {
					break Loop
				}
				tChan <- trailer
			default: // send out other tokens
				tChan <- v
			}

		default: // send out other item types
			tChan <- item
		}
	}
	if err != nil {
		tChan <- err
	}

	close(tChan)
}

func readObject(r peekingReader, ref *objectref) (*object, error) {
	o := object{refString: ref.refString}
	for {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return nil, v

		case token:
			switch v {
			case "stream":
				if l := o.streamLength(); l > 0 {
					s, err := r.ReadBytes(l)
					if err != nil {
						return nil, err
					}
					err = o.setStream(s)
					if err != nil {
						return nil, err
					}
					continue
				}
			case "endstream":
				continue
			case "endobj":
				return &o, nil
			}
		}

		if dict, ok := item.(dictionary); ok {
			o.dict = dict
		} else {
			o.values = append(o.values, item)
		}
	}
}

func readXref(r peekingReader) (xref, error) {
	var xrefStart, xrefEnd int
	var xref xref
	var number int
	var generation token
	var byteOffset int
	xrefCount := 1

	for {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return nil, v

		case token:
			if xrefCount == 1 {
				xrefStart, _ = strconv.Atoi(string(v))
			} else if xrefCount == 2 {
				xrefEnd, _ = strconv.Atoi(string(v))
				xref = make(map[string]xrefItem)
			} else if xrefCount >= 3 {
				rowNum := xrefCount/3 - 1
				switch xrefCount % 3 {
				case 0: // byte offset
					byteOffset, _ = strconv.Atoi(string(v))
					number = xrefStart + rowNum
				case 1: // generation number
					generation = v
				case 2: // xref type
					xref[fmt.Sprintf("%d %s", number, generation)] = xrefItem{byteOffset: byteOffset, xrefType: string(v)}
				}
			}
		}
		if xrefCount/3 == xrefEnd && xrefCount%3 == 2 { // at the end of the xref
			xrefCount = 0
			return xref, nil
		} else if xrefCount > 0 {
			xrefCount++
		}
	}
}

func readTrailer(r peekingReader) (rootnode, error) {
	for {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return "\x00", v

		case dictionary:
			if r, ok := v["/Root"].(*objectref); ok {
				return rootnode(r.refString), nil
			}
		}
	}
}

func readNext(r peekingReader) interface{} {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch b {
	case '(':
		v, err := readUntil(r, ')') // make into readText
		if err != nil {
			return err
		}
		r.ReadByte()                          // move read pointer past the ')'
		for bytes.HasSuffix(v, []byte(`\`)) { // ends with escape character so go to next end
			n, err := readUntil(r, ')')
			if err != nil {
				return err
			}
			v = append(v, ')')  // add the escaped ')'
			v = append(v, n...) // add the next characters too
		}
		return text(v)
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
		r.ReadByte() // move read pointer past the '>'
		return hexdata(v)
	case '[':
		v, err := readArray(r)
		if err != nil {
			return err
		}
		return v
	case '{':
		v, err := readUntil(r, '}')
		if err != nil {
			return err
		}
		r.ReadByte() // move read pointer past the '}'
		return codestream(v)
	case '/':
		v, err := readName(r)
		if err != nil {
			return err
		}
		return v
	case '%':
		v, err := readUntilAny(r, eolChars) // make into readComment
		if err != nil {
			return err
		}
		return comment(v)
	case '\x00', '\t', '\f', ' ', '\n', '\r':
		err := skipSpaces(r)
		if err != nil {
			return err
		}
		return readNext(r)
	case ']', ')', '>', '}':
		return end(b)
	default:
		token, oref, err := readTokenOrObjectReference(b, r)
		if err != nil {
			return err
		}
		if oref == nil {
			return token
		}
		return oref
	}
}

func isWhitespace(b byte) bool {
	return b == '\x00' || b == '\t' || b == '\f' || b == ' ' || b == '\n' || b == '\r'
}

func isDelimiter(b byte) bool {
	return b == '(' || b == ')' || b == '<' || b == '>' ||
		b == '[' || b == ']' || b == '{' || b == '}' || b == '/' || b == '%'
}

func isNumber(b byte) bool {
	return b >= '0' && b <= '9'
}

func readArray(r peekingReader) (array, error) {
	items := array{}
	for {
		item := readNext(r)
		if err, ok := item.(error); ok {
			return nil, errors.Wrap(err, fmt.Sprintf("error while reading array item"))
		}
		if end, ok := item.(end); ok && end == ']' {
			return items, nil
		}
		items = append(items, item)
	}
}

func readName(r peekingReader) (name, error) {
	p, err := r.Peek(1)
	if err != nil {
		return "\x00", err
	}
	if p[0] == '/' {
		r.ReadByte() // read past first '/'
	}
	v, err := readUntilAny(r, delimChars)
	if err != nil {
		return "\x00", err
	}
	if !bytes.HasPrefix(v, []byte{'/'}) { // include leading '/' if not included
		v = append([]byte{'/'}, v...)
	}

	return name(v), err
}

func (o *object) objectref(n name) *objectref {
	if oref, ok := o.search(n).(*objectref); ok {
		return oref
	}
	return nil
}

func (o *object) array(n name) array {
	if arr, ok := o.search(n).(array); ok {
		return arr
	}
	return nil
}

func (o *object) name(n name) name {
	if v, ok := o.search(n).(name); ok {
		return v
	}
	return "\x00"
}

func (o *object) int(n name) int {
	if v, ok := o.search(n).(token); ok {
		i, err := strconv.Atoi(string(v))
		if err != nil {
			return 0
		}
		return i
	}
	return 0
}

func (o *object) search(name name) interface{} {
	if o.dict == nil {
		return nil
	}
	if v, ok := o.dict[name]; ok {
		return v
	}
	return nil
}

func (o *object) pages() *objectref {
	if o.name("/Type") == "/Catalog" { // check to see if it is the root catalog
		return o.objectref("/Pages")
	}
	return nil
}

func (o *object) streamLength() int {
	return o.int("/Length")
}

func (o *object) hasTextStream() bool {
	if o.stream == nil {
		return false
	}
	t := o.name("/Type")
	return t == "\x00" || t != "/Font" && t != "/FontDescriptor" && t != "/XRef"
}

func (o *object) setStream(s stream) error {
	filter := o.name("/Filter")

	switch filter {
	//case "/ASCIIHexDecode":
	//case "/ASCII85Decode":
	//case "/LZWDecode":

	case "/FlateDecode":
		if o.name("/Type") == "/ObjStm" {
			o.isObjStm = true
			if f, ok := o.search("/First").(token); ok {
				o.first, _ = strconv.Atoi(string(f))
			}
			if n, ok := o.search("/N").(token); ok {
				o.n, _ = strconv.Atoi(string(n))
			}
		}
		buf := bytes.NewReader(s)
		r, err := zlib.NewReader(buf)
		if err != nil {
			o.stream = bytes.NewReader(s)
		}
		o.stream = r
	//case "/RunLengthDecode":

	//case "/CCITTFaxDecode":
	//case "/JBIG2Decode":
	//case "/DCTDecode":
	//case "/JPXDecode":
	//case "/Crypt":
	default:
		o.stream = bytes.NewReader(s)
	}
	return nil
}

func readDictionary(r peekingReader) (dictionary, error) {
	d := make(dictionary)
	for {
		name, err := readName(r)
		if err != nil {
			return nil, err
		}

		item := readNext(r)
		if err, ok := item.(error); ok {
			return nil, err
		}
		if end, ok := item.(end); ok && end == '>' {
			d[name] = null(true)
			r.ReadByte() // skip the second '>' too
			return d, nil
		}
		d[name] = item

		err = skipSpaces(r)
		if err != nil {
			return nil, err
		}
		p, err := r.Peek(2)
		if err != nil {
			return d, err
		}
		if string(p) == ">>" {
			r.ReadBytes(2) // move forward read pointer
			return d, nil
		}
	}
}

// readTokenOrObjectReference attempts to read an object reference if possible and returns
// a token if not possible. Object references consist of 3 parts:
//   1. number, 2. generation and 3. "R" or "obj"
func readTokenOrObjectReference(b byte, r peekingReader) (token, *objectref, error) {
	tok, err := readToken(b, r)
	if err != nil {
		return "\x00", nil, err
	}
	number, err := strconv.Atoi(string(tok))
	if err != nil {
		return tok, nil, nil
	}

	buf, err := r.Peek(8) // should be enough for generation and refType
	if err != nil {
		return tok, nil, nil
	}

	var g []byte
	var count = 0
	for _, n := range buf {
		count++
		if isNumber(n) { // can only be numbers
			g = append(g, n)
		} else if isWhitespace(n) {
			if len(g) != 0 {
				break
			}
		} else { // must be numeric or whitespace (before or after token)
			return tok, nil, nil
		}
	}

	var t []byte
	for i := count; i < 8; i++ {
		n := buf[i]
		count++
		if n == 'R' || n == 'o' || n == 'b' || n == 'j' { // only R or obj characters allowed
			t = append(t, n)
		} else if isWhitespace(n) { // can be whitespace before or after
			if len(t) != 0 {
				break
			}
		} else if isDelimiter(n) {
			count--
			break
		} else {
			return tok, nil, nil
		}
	}

	refType := string(t)
	if refType != "R" && refType != "obj" {
		return tok, nil, nil
	}
	generation, _ := strconv.Atoi(string(g))
	r.ReadBytes(count) // consume the bytes we used in the object reference
	return "\x00", &objectref{refString: fmt.Sprintf("%d %d", number, generation), refType: refType}, nil
}

func readToken(b byte, r peekingReader) (token, error) {
	tok, err := readUntilAny(r, delimChars)
	if err != nil {
		return "\x00", err
	}
	tok = append([]byte{b}, tok...)

	if string(tok) == "stream" { // special case for stream
		next, err := r.ReadByte()
		if err != nil {
			return "\x00", err
		}
		if next == '\r' { // EOL is \r, so take \n as well (section 3.2.7)
			next, err = r.ReadByte()
			if err != nil {
				return "\x00", err
			}
			if next != '\n' { // doesn't follow spec
				return "\x00", errors.New("expected \r\n EOL delimiter")
			}
		}
	}

	return token(tok), nil
}
