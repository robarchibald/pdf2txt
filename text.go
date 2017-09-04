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
	catalogs      map[string]*catalog
	pagesList     map[string]*pages
	pageList      map[string]*page
	fonts         map[string]*font
	cmaps         map[string]cmap
	contents      map[string][]textsection
	uncategorized map[string]*object
	objectstreams map[string]*object
	trailer       *trailer
	decodeError   error
}

type catalog struct {
	Pages string
}

type pages struct {
	Kids   []string
	isNull bool
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
	if d.decodeError != nil {
		fmt.Println("looks encrypted", d.decodeError)
	}
	//	if err = d.populate(); err != nil {
	//return nil, err
	//}
	return d.getText()
}

func parse(r io.Reader) (*document, error) {
	doc := &document{catalogs: make(map[string]*catalog), pagesList: make(map[string]*pages), pageList: make(map[string]*page),
		fonts: make(map[string]*font), cmaps: make(map[string]cmap), contents: make(map[string][]textsection),
		objectstreams: make(map[string]*object), uncategorized: make(map[string]*object), trailer: &trailer{}}

	tchan := make(chan interface{}, 100)
	go tokenize(newBufReader(r), tchan)

	for t := range tchan {
		if err := parseItem(t, doc); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

func parseItem(item interface{}, doc *document) error {
	switch v := item.(type) {
	case error:
		return v
	case *trailer:
		if v.rootRef != "" {
			doc.trailer.rootRef = v.rootRef
		}
		if v.decodeParms != nil {
			doc.trailer.decodeParms = v.decodeParms
		}
		if v.encryptRef != "" {
			doc.trailer.encryptRef = v.encryptRef
		}
	case *object:
		oType := v.name("/Type")
		switch oType {
		case "/Catalog":
			doc.catalogs[v.refString] = &catalog{v.objectref("/Pages").refString}

		case "/Pages":
			doc.pagesList[v.refString] = v.getPages()

		case "/Page":
			pItem := v.getPage()
			doc.pageList[v.refString] = pItem
			handlePageParent(pItem, v.refString, doc.pagesList)
			if doc.decodeError != nil {
				return nil
			}
			if err := handlePageContents(pItem, doc.contents, doc.uncategorized); err != nil {
				return err
			}

		case "/Font":
			f := v.getFont()
			doc.fonts[v.refString] = f
			if doc.decodeError != nil {
				return nil
			}
			if err := handleToUnicode(f, doc.cmaps, doc.uncategorized); err != nil {
				doc.decodeError = err
			}

		case "/ObjStm":
			if doc.decodeError != nil {
				doc.objectstreams[v.refString] = v
				return nil
			}
			err := v.decodeStream()
			if err != nil {
				doc.objectstreams[v.refString] = v
				doc.decodeError = err
				return nil
			}
			objs, err := v.getObjectStream()
			if err != nil {
				return err
			}
			for i := range objs {
				parseItem(objs[i], doc)
			}

		case "/XObject": // we don't need
		case "/FontDescriptor": // we don't need
		default:
			// something has already referenced this as content so save as content
			if _, ok := doc.contents[v.refString]; ok && doc.decodeError == nil {
				if err := v.saveContents(doc.contents); err != nil {
					doc.decodeError = err
				}

				// save cmap
			} else if _, ok := doc.cmaps[v.refString]; ok && doc.decodeError == nil {
				if err := v.saveCmap(doc.cmaps); err != nil {
					doc.decodeError = err
				}
			} else {
				doc.uncategorized[v.refString] = v
			}
		}
	}
	return nil
}

// according to the spec, we are supposed to read the trailer to find the
// root pages object and then iterate through children to find all children
func (d *document) getText() (io.Reader, error) {
	var buf bytes.Buffer
	catalog, ok := d.catalogs[d.trailer.rootRef]
	if !ok {
		return nil, errors.New("unable to find catalog")
	}

	for _, page := range d.getPages(catalog.Pages) { // get page objects
		buf.WriteString(d.getPageText(page))
		buf.WriteString("\n")
	}
	return &buf, nil
}

// Loop through pages and page nodes to get all the pages
func (d *document) getPages(refString string) []*page {
	if node, ok := d.pagesList[refString]; ok { // this is a pages node so loop through kids
		var pages []*page
		for i := range node.Kids {
			pages = append(pages, d.getPages(node.Kids[i])...)
		}
		return pages
	} else if node, ok := d.pageList[refString]; ok { // this is a page node so return page
		return []*page{node}
	}
	return []*page{}
}

func (d *document) getPageText(p *page) string {
	var buf bytes.Buffer
	if p == nil || p.Contents == nil {
		fmt.Println("nil p")
	}
	for _, cref := range p.Contents { // get content
		c := d.contents[cref]
		for sIndex := range c { // get text sections
			section := c[sIndex]
			for ai := range section.textArray {
				item := section.textArray[ai]
				switch t := item.(type) {
				case hexdata:
					font := d.fonts[p.Fonts[section.fontName]]
					var cmap map[hexdata]string
					if font != nil && font.ToUnicode != "" && d.cmaps[font.ToUnicode] != nil {
						cmap = d.cmaps[font.ToUnicode]
					}
					for ci := 0; ci+2 <= len(t); ci += 2 {
						if cmap != nil {
							buf.WriteString(cmap[t[ci:ci+2]])
						} else {
							c, _ := strconv.ParseInt(string(t[ci:ci+2]), 16, 16)
							buf.WriteString(fmt.Sprintf("%c", c))
						}
					}
				case string:
					buf.WriteString(t)
				}
			}
		}
	}
	return buf.String()
}

func handlePageContents(pItem *page, contents map[string][]textsection, uncategorized map[string]*object) error {
	for i := range pItem.Contents {
		cref := pItem.Contents[i]
		// contents already available, so get text
		if cObj, ok := uncategorized[cref]; ok {
			if err := cObj.saveContents(contents); err != nil {
				return err
			}
			delete(uncategorized, cref)

			// haven't seen contents yet, so just flag it for later retrieval
		} else {
			contents[cref] = nil
		}
	}
	return nil
}

func handlePageParent(pItem *page, pageRef string, pagesList map[string]*pages) {
	if pItem.Parent != "" {
		if pagesItem, ok := pagesList[pItem.Parent]; ok {
			if pagesItem.isNull { // null object so add this reference to the list of kids
				pagesItem.Kids = append(pagesItem.Kids, pageRef)
			}
		} else {
			pagesList[pItem.Parent] = &pages{Kids: []string{pageRef}, isNull: true}
		}
	}
}

func handleToUnicode(f *font, cmaps map[string]cmap, uncategorized map[string]*object) error {
	if f.ToUnicode != "" {
		// cmap already available, so create
		if u, ok := uncategorized[f.ToUnicode]; ok {
			if err := u.saveCmap(cmaps); err != nil {
				return err
			}
			delete(uncategorized, f.ToUnicode)

			// haven't seen cmap yet, so just flag for later
		} else {
			cmaps[f.ToUnicode] = nil
		}
	}
	return nil
}

func getTextSections(r peekingReader) ([]textsection, error) {
	sections := []textsection{}
	var font name
	var prevArray array
	var prev interface{}
	var prevName name

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
			case "Tf":
				font = prevName
			case "TJ":
				sections = append(sections, textsection{fontName: font, textArray: append(prevArray, " ")})
			case "T*":
				sections = append(sections, textsection{fontName: font, textArray: []interface{}{"\n"}})
			case "Tj":
				sections = append(sections, textsection{fontName: font, textArray: []interface{}{prev}})
			}

		case array:
			prevArray = v
		case text, hexdata:
			prev = v
		case name:
			prevName = v
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
			if v == io.EOF {
				return cmap, nil
			}
			return nil, v

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
