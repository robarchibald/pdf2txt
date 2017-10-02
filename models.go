package pdf2txt

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strconv"

	"github.com/EndFirstCorp/peekingReader"
)

func (o *object) getFont() *font {
	font := font{Encoding: o.name("/Encoding")}
	if u := o.objectref("/ToUnicode"); u != nil {
		font.ToUnicode = u.refString
	}
	return &font
}

func (o *object) getPages() *pages {
	k := o.array("/Kids")
	kids := make([]string, len(k))
	for i := range k {
		if oref, ok := k[i].(*objectref); ok {
			kids[i] = oref.refString
		}
	}
	return &pages{Kids: kids}
}

func (o *object) getPage() *page {
	page := page{Fonts: make(map[name]string)}
	if res, ok := o.search("/Resources").(dictionary); ok {
		if fonts, ok := res["/Font"].(dictionary); ok {
			for key, value := range fonts {
				if oref, ok := value.(*objectref); ok {
					page.Fonts[key] = oref.refString
				}
			}
		}
	}
	// Contents can be either a single object reference
	// or an array of object references
	if c := o.search("/Contents"); c != nil {
		if co, ok := c.(*objectref); ok {
			page.Contents = []string{co.refString}
		} else if ca, ok := c.(array); ok {
			for i := range ca {
				if cao, ok := ca[i].(*objectref); ok {
					page.Contents = append(page.Contents, cao.refString)
				}
			}
		}
	}
	if p := o.objectref("/Parent"); p != nil {
		page.Parent = p.refString
	}
	return &page
}

func (d dictionary) String() string {
	var buf bytes.Buffer
	buf.WriteString("<<")
	for key := range d {
		buf.WriteString("\n  ")
		buf.WriteString(string(key))
		buf.WriteString(fmt.Sprintf(" %v", d[key]))
	}
	buf.WriteString("\n>>")
	return buf.String()
}

func (o *object) String() string {
	var buf bytes.Buffer
	buf.WriteString(o.refString)
	buf.WriteString(" obj\n")
	if o.dict != nil {
		buf.WriteString(fmt.Sprintf("%v\n", o.dict))
	}
	for i := range o.values {
		buf.WriteString(fmt.Sprintf("%v\n", o.values[i]))
	}
	buf.WriteString("endobj\n")
	return buf.String()
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
		i, _ := strconv.Atoi(string(v))
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

func (o *object) streamLength() int {
	return o.int("/Length")
}

func (o *object) decodeStream() error {
	if o.isStreamDecoded {
		return nil
	}
	filter := o.name("/Filter")

	switch filter {
	//case "/ASCIIHexDecode":
	//case "/ASCII85Decode":
	//case "/LZWDecode":

	case "/FlateDecode":
		buf := bytes.NewReader(o.stream)
		r, err := zlib.NewReader(buf)
		if err != nil {
			return err
		}

		var out bytes.Buffer
		if _, err := out.ReadFrom(r); err != nil {
			return err
		}
		o.isStreamDecoded = true
		o.stream = out.Bytes()
		return nil
	//case "/RunLengthDecode":

	//case "/CCITTFaxDecode":
	//case "/JBIG2Decode":
	//case "/DCTDecode":
	//case "/JPXDecode":
	//case "/Crypt":
	default:
		return nil
	}
}

func (o *object) isTrailer() bool {
	return o.objectref("/Root") != nil
}

func (o *object) getObjectStream() ([]*object, error) {
	n := o.search("/N").(token)
	numObjs, _ := strconv.Atoi(string(n))

	objs := make([]*object, numObjs)
	r := peekingReader.NewMemReader(o.stream)
	for i := 0; i < numObjs; i++ {
		number := readNext(r)
		refString := fmt.Sprintf("%v 0", number)
		objs[i] = &object{refString: refString}

		readNext(r) // offset info (we don't need)
	}
	for i := 0; i < numObjs; i++ {
		obj := readNext(r)
		switch v := obj.(type) {
		case error:
			return nil, v
		case dictionary:
			objs[i].dict = v
		default:
			objs[i].values = []interface{}{v}
		}
	}
	return objs, nil
}

func (o *object) saveContents(contents map[string][]textsection) error {
	err := o.decodeStream()
	if err != nil {
		return err
	}
	sections, err := getTextSections(peekingReader.NewMemReader(o.stream))
	if err != nil {
		return err
	}
	contents[o.refString] = sections
	return nil
}

func (o *object) saveCmap(cmaps map[string]cmap) error {
	if err := o.decodeStream(); err != nil {
		return err
	}
	cmap, err := getCmap(peekingReader.NewMemReader(o.stream))
	if err != nil {
		return err
	}
	cmaps[o.refString] = cmap
	return nil
}
