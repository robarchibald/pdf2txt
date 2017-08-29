package pdf2txt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type document struct {
	catalogs      []*catalog
	pagesList     map[string]*pages
	pageOrder     []string
	pageList      map[string]*page
	fonts         map[string]*font
	cmaps         map[string]cmap
	contents      map[string][]textsection
	uncategorized map[string]*object
	root          rootnode
}

type catalog struct {
	Pages string
}

type pages struct {
	Count int
	Kids  []string
}

type page struct {
	Fonts    map[name]string
	Contents []string
	Parent   string
}

type font struct {
	Encoding  name
	ToUnicode string
}

// Text extracts text from an io.Reader stream of a PDF file
// and outputs it into a new io.Reader filled with the text
// contained in the PDF file.
func Text(r io.Reader) (io.Reader, error) {
	d, err := parse(r)
	if err != nil {
		return nil, err
	}
	if err = d.populate(); err != nil {
		return nil, err
	}
	return d.getText()
}

func parse(r io.Reader) (*document, error) {
	catalogs := []*catalog{}
	pagesList := make(map[string]*pages)
	pageList := make(map[string]*page)
	fonts := make(map[string]*font)
	cmaps := make(map[string]cmap)
	contents := make(map[string][]textsection)
	uncategorized := make(map[string]*object)
	var root rootnode

	tchan := make(chan interface{}, 100)
	go Tokenize(newBufReader(r), tchan)

	for t := range tchan {
		switch v := t.(type) {
		case rootnode:
			root = v
		case *object:
			oType := v.name("/Type")
			switch oType {
			case "/Catalog":
				catalogs = append(catalogs, &catalog{v.objectref("/Pages").refString})

			case "/Pages":
				pagesList[v.refString] = getPages(v)

			case "/Page":
				pageList[v.refString] = getPage(v)
				if pageList[v.refString].Contents != nil {
					for i := range pageList[v.refString].Contents {
						item := pageList[v.refString].Contents[i]
						contents[item] = nil
					}
				}

			case "/Font":
				fonts[v.refString] = getFont(v)
				if fonts[v.refString].ToUnicode != "" {
					cmaps[fonts[v.refString].ToUnicode] = nil
				}

			case "/XObject": // we don't need
			case "/FontDescriptor": // we don't need
			default:
				if _, ok := contents[v.refString]; ok { // something has already referenced this as content so save as content
					sections, err := getTextSections(newMemReader(v.stream))
					if err != nil {
						fmt.Println("error getting textsection", err)
						return nil, err // maybe I shouldn't error completely if one page is bad
					}
					contents[v.refString] = sections
				} else if _, ok := cmaps[v.refString]; ok {
					cmap, err := getCmap(newMemReader(v.stream))
					if err != nil {
						fmt.Println("error getting cmap", err)
						return nil, err // maybe I shouldn't error completely if one page is bad
					}
					cmaps[v.refString] = cmap
				} else {
					uncategorized[v.refString] = v
				}
			}
		}
	}
	return &document{catalogs: catalogs, pagesList: pagesList, pageList: pageList, fonts: fonts, cmaps: cmaps,
		contents: contents, uncategorized: uncategorized, root: root}, nil
}

func (d *document) populate() error {
	// populate pages objects
	for i := range d.catalogs {
		catalog := d.catalogs[i]
		if _, ok := d.pagesList[catalog.Pages]; !ok {
			if p, ok := d.uncategorized[catalog.Pages]; ok {
				d.pagesList[catalog.Pages] = getPages(p)
				delete(d.uncategorized, catalog.Pages)
			}
		}

		// infer page objects from parent property if needed
		pagesObj := d.pagesList[catalog.Pages]
		var kids []string
		if pagesObj != nil && len(pagesObj.Kids) != 0 {
			kids = pagesObj.Kids
		} else {
			for ref := range d.pageList { // NOTE: this will be in random order. Not correct!!!
				if d.pageList[ref].Parent == ref {
					kids = append(kids, ref)
				}
			}
			d.pagesList[catalog.Pages] = &pages{Count: len(kids), Kids: kids}
		}

		// loop through page objects
		for pCount := range kids {
			pageRef := kids[pCount]
			if d.pageList[pageRef] == nil && d.uncategorized[pageRef] != nil {
				page := getPage(d.uncategorized[pageRef])
				d.pageList[pageRef] = page
				delete(d.uncategorized, pageRef)
			}
			page := d.pageList[pageRef]
			d.pageOrder = append(d.pageOrder, pageRef)
			if page == nil {
				continue
			}

			// get page contents
			for cIndex := range page.Contents {
				contentsRef := page.Contents[cIndex]
				if c, ok := d.contents[contentsRef]; (!ok || c == nil) && d.uncategorized[contentsRef] != nil {
					c, err := getTextSections(newMemReader(d.uncategorized[contentsRef].stream))
					if err != nil {
						return err
					}
					d.contents[contentsRef] = c
					delete(d.uncategorized, contentsRef)
				}
			}

			// get page fonts
			for name := range page.Fonts {
				fontRef := page.Fonts[name]
				if _, ok := d.fonts[fontRef]; !ok && d.uncategorized[fontRef] != nil {
					d.fonts[fontRef] = getFont(d.uncategorized[fontRef])
					delete(d.uncategorized, fontRef)
				}
			}
		}
	}

	// populate cmaps
	for ref := range d.fonts {
		font := d.fonts[ref]
		cmapRef := font.ToUnicode
		if cmap, ok := d.cmaps[cmapRef]; (!ok || cmap == nil) && cmapRef != "" && d.uncategorized[cmapRef] != nil {
			cmap, err := getCmap(newMemReader(d.uncategorized[cmapRef].stream))
			if err != nil {
				return err
			}
			d.cmaps[cmapRef] = cmap
			delete(d.uncategorized, cmapRef)
		}
	}
	//fmt.Println("catalogs", catalogs)
	//fmt.Println("pagesList", pagesList)
	//fmt.Println("pageList", pageList)
	//fmt.Println("fonts", fonts)
	//fmt.Println("contents", contents)
	//fmt.Println("cmaps", cmaps)
	//fmt.Println("uncategorized", uncategorized)
	return nil
}

func (d *document) getText() (io.Reader, error) {
	var buf bytes.Buffer
	for _, pageRef := range d.pageOrder { // get pages
		page := d.pageList[pageRef]
		for _, cref := range page.Contents { // get content
			c := d.contents[cref]
			for sIndex := range c { // get text sections
				section := c[sIndex]
				buf.WriteString(string(section.text))
				for ai := range section.textArray {
					item := section.textArray[ai]
					switch t := item.(type) {
					case hexdata:
						font := d.fonts[page.Fonts[section.fontName]]
						var cmap map[hexdata]string
						if font != nil && font.ToUnicode != "" && d.cmaps[font.ToUnicode] != nil {
							cmap = d.cmaps[font.ToUnicode]
						}
						for ci := 0; ci < len(t); ci += 4 {
							if cmap != nil {
								buf.WriteString(cmap[t[ci:ci+4]])
							} else {
								c, _ := strconv.ParseInt(string(t[ci:ci+4]), 16, 16)
								buf.WriteString(fmt.Sprintf("%c", c))
							}
						}
					case text:
						buf.WriteString(string(t))
					}
				}
			}
		}
	}
	return &buf, nil
}

func getPages(o *object) *pages {
	k := o.array("/Kids")
	kids := make([]string, len(k))
	for i := range k {
		if oref, ok := k[i].(*objectref); ok {
			kids[i] = oref.refString
		}
	}
	return &pages{Kids: kids, Count: o.int("/Count")}
}

func getPage(o *object) *page {
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

func getFont(o *object) *font {
	font := font{Encoding: o.name("/Encoding")}
	if u := o.objectref("/ToUnicode"); u != nil {
		font.ToUnicode = u.refString
	}
	return &font
}

func (o *object) getObjectStream() ([]*object, error) {
	if !o.isObjStm {
		return nil, errors.New("Not a valid object stream")
	}
	objs := make([]*object, o.n)
	r := newMemReader(o.stream)
	for i := 0; i < o.n; i++ {
		number := readNext(r)
		refString := fmt.Sprintf("%v 0", number)
		objs[i] = &object{refString: refString}

		readNext(r) // offset info (we don't need)
	}
	for i := 0; i < o.n; i++ {
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

func getTextSections(r peekingReader) ([]textsection, error) {
	sections := []textsection{}
	var t textsection
	var font name
	stack := stack{}

	for {
		item := readNext(r)

		switch v := item.(type) {
		case error:
			if v == io.EOF {
				return sections, nil
			}
			return nil, v

		case token:
			switch v {
			case "BT":
				t = textsection{}
			case "Tf":
				if name, ok := stack.Pop().(name); ok {
					font = name
				}
			case "TJ":
				if textArray, ok := stack.Pop().(array); ok {
					t.textArray = textArray
				}
			case "Tj":
				if text, ok := stack.Pop().(text); ok {
					t.text = text
				}
			case "ET":
				t.fontName = font // use the current global text state
				sections = append(sections, t)
			}

		default:
			stack.Push(item)
		}
	}
}

func getCmap(r peekingReader) (cmap, error) {
	cmap := make(cmap)
	var prev token

	for {
		item := readNext(r)

		switch v := item.(type) {
		case error:
			if v != io.EOF {
				return nil, v
			}

		case token:
			switch v {
			case "begincodespacerange":
				length, _ := strconv.Atoi(string(prev))
				for i := 0; i < length*2; i++ {

				}
			case "beginbfchar":
				bfc, err := readbfchar(r, prev)
				if err != nil {
					return nil, err
				}
				for key, value := range bfc {
					cmap[key] = value
				}
			case "beginbfrange":
				bfr, err := readbfrange(r, prev)
				if err != nil {
					return nil, err
				}
				for key, value := range bfr {
					cmap[key] = value
				}
			case "endcmap":
				return cmap, nil
			default:
				prev = v
			}
		}
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
