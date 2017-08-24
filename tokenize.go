package pdf2txt

import (
	"bufio"
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
func Tokenize(pdf io.Reader, tChan chan interface{}) {
	br := bufio.NewReader(pdf)
	var b byte
	var err error
	var ok bool

	count := 0
	for b, err = br.ReadByte(); err == nil; b, err = br.ReadByte() {
		item := readNext(b, br)
		if err, ok = item.(error); ok {
			break
		}

		// read an object
		if oref, ok := item.(*objectref); ok && oref.refType == "obj" {
			var obj *object
			obj, err = readObject(br, pdf, oref)
			if err != nil {
				break
			}
			tChan <- obj
			continue
		}

		// handle xref section
		if tok, ok := item.(token); ok {
			switch tok {
			case "xref":
				var xref xref
				xref, err = readXref(b, br)
				if err != nil {
					break
				}
				tChan <- xref
				continue
			case "BT":
				fmt.Println("handle text")
			case "begincmap":
				readCmap(br)
				continue
			}
		}
		tChan <- item

		if count >= 3000 {
			tChan <- errors.New("looks like infinite loop. exiting")
			break
		}
		count++
	}
	if err != nil {
		tChan <- err
	}

	close(tChan)
}

func readCmap(r *bufio.Reader) (cmap, error) {
	cmap := make(cmap)
	var prev interface{}

	for b, err := r.ReadByte(); err == nil; b, err = r.ReadByte() {
		item := readNext(b, r)
		if err, ok := item.(error); ok {
			return nil, err
		}

		if tok, ok := item.(token); ok {
			switch tok {
			case "begincodespacerange":
				length, _ := strconv.Atoi(string(prev.(token)))
				for i := 0; i < length*2; i++ {

				}
			case "beginbfchar":
				cmap, err := readbfchar(r, prev.(token))
				fmt.Println("bfchar", cmap, err)
			case "beginbfrange":
				cmap, err := readbfrange(r, prev.(token))
				fmt.Println("bfrange", cmap, err)
			case "endcmap":
				fmt.Println("end cmap")
				return cmap, nil
			}
		}
		prev = item
	}
	return nil, errors.New("didn't expect to get here")
}

func readbfchar(r *bufio.Reader, length token) (cmap, error) {
	cmap := make(cmap)
	l, _ := strconv.Atoi(string(length))
	var lastKey hexdata
	for i := 0; i < l*2; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		item := readNext(b, r)
		if err, ok := item.(error); ok {
			return nil, err
		}

		if data, ok := item.(hexdata); ok {
			switch i % 2 {
			case 0: // first item is the key
				lastKey = data
				cmap[data] = ""
			case 1: // second item is the value
				num, _ := strconv.ParseInt(string(data), 16, 16)
				repl := fmt.Sprintf("%c", num)
				cmap[lastKey] = repl
			}
		} else {
			return nil, errors.New("invalid bfchar data")
		}
	}
	return cmap, nil
}

func readbfrange(r *bufio.Reader, length token) (cmap, error) {
	cmap := make(cmap)
	l, _ := strconv.Atoi(string(length))
	var start, end int64
	var digits int
	for i := 0; i < l*3; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		item := readNext(b, r)
		if err, ok := item.(error); ok {
			return nil, err
		}

		if data, ok := item.(hexdata); ok {
			switch i % 3 {
			case 0: // range start
				digits = len(string(data))
				start, _ = strconv.ParseInt(string(data), 16, 16)
			case 1: // range end
				end, _ = strconv.ParseInt(string(data), 16, 16)
			case 2: // values
				repl, _ := strconv.ParseInt(string(data), 16, 16)
				var count int64
				for i := start; i <= end; i++ {
					format := fmt.Sprintf("%%0%dx", digits) // format for however many digits we originally had
					cmap[hexdata(strings.ToUpper(fmt.Sprintf(format, i)))] = fmt.Sprintf("%c", repl+count)
					count++
				}
			}
		} else {
			return nil, errors.New("invalid bfrange data")
		}
	}
	return cmap, nil
}

func readObject(r *bufio.Reader, pdf io.Reader, ref *objectref) (*object, error) {
	o := object{number: ref.number, generation: ref.generation}
	for b, err := r.ReadByte(); err == nil; b, err = r.ReadByte() {
		item := readNext(b, r)
		if err, ok := item.(error); ok {
			return nil, err
		}

		if tok, ok := item.(token); ok {
			switch tok {
			case "stream":
				if l := o.streamLength(); l > 0 {
					var s stream
					s, err = readStream(r, pdf, l)
					if err != nil {
						break
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
	return nil, errors.New("unexpected end")
}

func readXref(b byte, r *bufio.Reader) (xref, error) {
	var xrefStart, xrefEnd int
	var xref xref
	xrefCount := 1

	for {
		item := readNext(b, r)
		if err, ok := item.(error); ok {
			return nil, err
		}

		if tok, ok := item.(token); ok {
			if xrefCount == 1 {
				xrefStart, _ = strconv.Atoi(string(tok))
			} else if xrefCount == 2 {
				xrefEnd, _ = strconv.Atoi(string(tok))
				xref = make([]xrefItem, xrefEnd-xrefStart)
			} else if xrefCount >= 3 {
				rowNum := xrefCount/3 - 1
				switch xrefCount % 3 {
				case 0: // byte offset
					byteOffset, _ := strconv.Atoi(string(tok))
					xref[rowNum].byteOffset = byteOffset
					xref[rowNum].number = xrefStart + rowNum
				case 1: // generation number
					generation, _ := strconv.Atoi(string(tok))
					xref[rowNum].generation = generation
				case 2: // xref type
					xref[rowNum].xrefType = string(tok)
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

func readNext(b byte, r *bufio.Reader) interface{} {
	switch b {
	case '(':
		v, err := readUntil(r, ')') // make into readText
		if err != nil {
			return err
		}
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
		return codestream(v)
	case '/':
		r.UnreadByte() // include the starting slash too
		v, err := readName(r)
		if err != nil {
			return err
		}
		return v
	case '%':
		v, _, err := readUntilAny(r, eolChars) // make into readComment
		if err != nil {
			return err
		}
		return comment(v)
	case '\x00', '\t', '\f', ' ', '\n', '\r':
		err := skipSpaces(r)
		if err != nil {
			return err
		}
		next, err := r.ReadByte()
		if err != nil {
			return err
		}
		return readNext(next, r)
	// the only way we should've gotten here is from inside a dictionary calling readNext when there is no value
	// this is known as the nullValue for pdfs
	case ')', '>', ']', '}':
		r.UnreadByte() // get back the end character
		return null(true)
	default:
		r.UnreadByte() // include first character in token
		objectref, err := readObjectReference(r)
		if err == nil {
			return objectref
		}
		token, err := readToken(r)
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

func readStream(br *bufio.Reader, r io.Reader, length int) (stream, error) {
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

func readArray(r *bufio.Reader) (array, error) {
	items := array{}
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
		if item == null(true) && (b == '>' || b == ')' || b == ']' || b == '}') {
			r.ReadByte()
			fmt.Println("bad place. skipping character or we'll be in infinte loop")
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
		if strings.Contains(string(key), "XObject") || strings.Contains(string(key), "Image") {
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
				continue
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

func readDictionary(r *bufio.Reader) (dictionary, error) {
	d := make(dictionary)
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
			r.Read(p) // move forward read pointer
			return d, nil
		}
	}
}

func readObjectReference(r *bufio.Reader) (*objectref, error) {
	// the VAST majority of object references are just 9 bytes long. We'll get 12 bytes to be sure
	// number     - 3 digits (allows up to 999 objects)
	// generation - 1 digit (up to 9 generations)
	// R or obj   - 3 digits max
	// spaces     - 2
	buf, err := r.Peek(12)
	if err != nil {
		return nil, err
	}

	tokens := []token{}
	var cur bytes.Buffer
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
		r.Read(make([]byte, bytesUsed)) // consume the bytes we used in the object reference
		return &objectref{number: number, generation: generation, refType: string(tokens[2])}, nil
	}
	return nil, errors.New("invalid object reference")
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
