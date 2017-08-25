package pdf2txt

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"strconv"
	"strings"

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
type xref []xrefItem
type cmap map[hexdata]string
type objectref struct {
	number     int
	generation int
	refType    string
}
type object struct {
	number     int
	generation int
	values     []interface{}
	dict       dictionary
	stream     io.Reader
}
type xrefItem struct {
	number     int
	byteOffset int
	generation int
	xrefType   string
}
type textsection struct {
	fontName  string
	textArray array
	text      text
}

var spaceChars = []byte{'\x00', '\t', '\f', ' ', '\n', '\r'}
var eolChars = []byte{'\r', '\n'}
var delimChars = append(spaceChars, '(', ')', '<', '>', '[', ']', '{', '}', '/', '%')

// Tokenize reads through the entire PDF document and adds a token
// to the tChan every time it encounters a token
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
				continue
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
				continue
			case "BT":
				var textsection *textsection
				textsection, err = readTextSection(r)
				if err != nil {
					break Loop
				}
				tChan <- textsection
				continue
			case "begincmap":
				var cmap cmap
				cmap, err = readCmap(r)
				if err != nil {
					break Loop
				}
				tChan <- cmap
				continue
			}
		}
		tChan <- item
	}

	close(tChan)
}

func readTextSection(r peekingReader) (*textsection, error) {
	t := &textsection{}
	stack := stack{}

	for {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return nil, v

		case token:
			switch v {
			case "Tf":
				stack.Pop() // font size
				if name, ok := stack.Pop().(name); ok {
					t.fontName = string(name)
				}
				continue
			case "TJ":
				if textArray, ok := stack.Pop().(array); ok {
					t.textArray = textArray
				}
				continue
			case "Tj":
				if text, ok := stack.Pop().(text); ok {
					t.text = text
				}
				continue
			case "ET":
				return t, nil
			}
		}
		stack.Push(item)
	}
}

func readCmap(r peekingReader) (cmap, error) {
	cmap := make(cmap)
	var prev interface{}

	for {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return nil, v

		case token:
			switch v {
			case "begincodespacerange":
				length, _ := strconv.Atoi(string(prev.(token)))
				for i := 0; i < length*2; i++ {

				}
			case "beginbfchar":
				cmap, err := readbfchar(r, prev.(token))
				if err != nil {
					return nil, err
				}
				if cmap != nil {

				}
			case "beginbfrange":
				cmap, err := readbfrange(r, prev.(token))
				if err != nil {
					return nil, err
				}
				if cmap != nil {

				}
			case "endcmap":
				return cmap, nil
			}
		}
		prev = item
	}
}

func readbfchar(r peekingReader, length token) (cmap, error) {
	cmap := make(cmap)
	l, _ := strconv.Atoi(string(length))
	var lastKey hexdata
	for i := 0; i < l*2; i++ {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return nil, v

		case hexdata:
			switch i % 2 {
			case 0: // first item is the key
				lastKey = v
				cmap[v] = ""
			case 1: // second item is the value
				num, _ := strconv.ParseInt(string(v), 16, 16)
				repl := fmt.Sprintf("%c", num)
				cmap[lastKey] = repl
			}

		default:
			return nil, errors.New("invalid bfchar data")
		}
	}
	return cmap, nil
}

func readbfrange(r peekingReader, length token) (cmap, error) {
	cmap := make(cmap)
	l, _ := strconv.Atoi(string(length))
	var start, end int64
	var digits int

	for i := 0; i < l*3; i++ {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return nil, v

		case hexdata:
			switch i % 3 {
			case 0: // range start
				digits = len(string(v))
				start, _ = strconv.ParseInt(string(v), 16, 16)
			case 1: // range end
				end, _ = strconv.ParseInt(string(v), 16, 16)
			case 2: // values
				repl, _ := strconv.ParseInt(string(v), 16, 16)
				var count int64
				for i := start; i <= end; i++ {
					format := fmt.Sprintf("%%0%dx", digits) // format for however many digits we originally had
					cmap[hexdata(strings.ToUpper(fmt.Sprintf(format, i)))] = fmt.Sprintf("%c", repl+count)
					count++
				}
			}

		default:
			return nil, errors.New("invalid bfrange data")
		}
	}
	return cmap, nil
}

func readObject(r peekingReader, ref *objectref) (*object, error) {
	o := object{number: ref.number, generation: ref.generation}
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
					o.setStream(s)
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
				xref = make([]xrefItem, xrefEnd-xrefStart)
			} else if xrefCount >= 3 {
				rowNum := xrefCount/3 - 1
				switch xrefCount % 3 {
				case 0: // byte offset
					byteOffset, _ := strconv.Atoi(string(v))
					xref[rowNum].byteOffset = byteOffset
					xref[rowNum].number = xrefStart + rowNum
				case 1: // generation number
					generation, _ := strconv.Atoi(string(v))
					xref[rowNum].generation = generation
				case 2: // xref type
					xref[rowNum].xrefType = string(v)
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
		objectref, err := readObjectReference(b, r)
		if err == nil {
			return objectref
		}
		token, err := readToken(b, r)
		if err != nil {
			return err
		}
		return token
	}
}

func isWhitespace(b byte) bool {
	return b == '\x00' || b == '\t' || b == '\f' || b == ' ' || b == '\n' || b == '\r'
}

func isDelimiter(b byte) bool {
	return b == '(' || b == ')' || b == '<' || b == '>' ||
		b == '[' || b == ']' || b == '{' || b == '}' || b == '/' || b == '%'
}

func isRegular(b byte) bool {
	return !isWhitespace(b) && !isDelimiter(b)
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
	endChars := append(spaceChars, '(', ')', '<', '>', '[', ']', '{', '}', '%') // any except /
	v, err := readUntilAny(r, endChars)
	if err != nil {
		return "\x00", err
	}
	if !bytes.HasPrefix(v, []byte{'/'}) { // include leading '/' if not included
		v = append([]byte{'/'}, v...)
	}

	return name(v), err
}

func (o *object) streamLength() int {
	for key, value := range o.dict {
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

func (o *object) hasTextStream() bool {
	if o.stream == nil {
		return false
	}
	for key := range o.dict {
		if o.number == 12 || o.number == 250 || o.number == 254 || o.number == 256 || o.number == 258 || o.number == 262 ||
			o.number == 264 || o.number == 268 || o.number == 270 || o.number == 272 || o.number == 276 || o.number == 277 ||
			strings.Contains(string(key), "XObject") || strings.Contains(string(key), "Image") || strings.Contains(string(key), "Metadata") || strings.Contains(string(key), "XML") || strings.Contains(string(key), "XRef") {
			return false
		}
	}
	return true
}

func (o *object) setStream(s stream) error {
	for key := range o.dict {
		name := string(key)
		items := strings.Split(name, "/")
		for _, decode := range items {
			if !strings.Contains(string(key), "Decode") && !strings.Contains(string(key), "Crypt") {
				o.stream = bytes.NewBuffer(s)
			}

			switch decode {
			case "ASCIIHexDecode":
			case "ASCII85Decode":
			case "LZWDecode":

			case "FlateDecode":
				buf := bytes.NewBuffer(s)
				r, err := zlib.NewReader(buf)
				if err != nil {
					return err
				}
				o.stream = r
			case "RunLengthDecode":

			case "CCITTFaxDecode":
			case "JBIG2Decode":
			case "DCTDecode":
			case "JPXDecode":
			case "Crypt":
			}
		}
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

func readObjectReference(b byte, r peekingReader) (*objectref, error) {
	// the VAST majority of object references are just 9 bytes long. We'll get 12 bytes to be sure
	// number     - 3 digits (allows up to 999 objects)
	// generation - 1 digit (up to 9 generations)
	// R or obj   - 3 digits max
	// spaces     - 2
	var cur bytes.Buffer
	cur.WriteByte(b) // add first character to the current token
	buf, err := r.Peek(11)
	if err != nil {
		return nil, err
	}

	tokens := []token{}
	var bytesUsed = 0
	for _, b := range buf {
		bytesUsed++
		if isRegular(b) {
			cur.WriteByte(b)
		} else {
			if cur.Len() > 0 {
				tokens = append(tokens, token(cur.Bytes()))
				cur.Reset()
			}

			if isDelimiter(b) { // we've gone as far as we can
				bytesUsed-- // don't count delimiter in bytes used
				break
			}
			if len(tokens) == 3 {
				break
			}
		}
	}
	if len(tokens) >= 3 && (tokens[2] == "R" || tokens[2] == "obj") {
		number, err := strconv.Atoi(string(tokens[0]))
		if err != nil {
			return nil, err
		}
		generation, err := strconv.Atoi(string(tokens[1]))
		if err != nil {
			return nil, err
		}
		r.ReadBytes(bytesUsed) // consume the bytes we used in the object reference
		return &objectref{number: number, generation: generation, refType: string(tokens[2])}, nil
	}
	return nil, errors.New("invalid object reference")
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
