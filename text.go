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
	//	if err = d.populate(); err != nil {
	//return nil, err
	//}
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
	go tokenize(newBufReader(r), tchan)

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
				pagesList[v.refString] = v.getPages()

			case "/Page":
				pItem := v.getPage()
				pageList[v.refString] = pItem
				if err := handlePageContents(pItem, contents, uncategorized); err != nil {
					return nil, err
				}
				handlePageParent(pItem, v.refString, pagesList)

			case "/Font":
				f := v.getFont()
				fonts[v.refString] = f
				if err := handleToUnicode(f, cmaps, uncategorized); err != nil {
					return nil, err
				}

			case "/XObject": // we don't need
			case "/FontDescriptor": // we don't need
			default:
				// something has already referenced this as content so save as content
				if _, ok := contents[v.refString]; ok {
					if err := v.saveContents(contents); err != nil {
						return nil, err
					}

					// save cmap
				} else if _, ok := cmaps[v.refString]; ok {
					if err := v.saveCmap(cmaps); err != nil {
						return nil, err
					}
				} else {
					uncategorized[v.refString] = v
				}
			}
		}
	}
	return &document{catalogs: catalogs, pagesList: pagesList, pageList: pageList, fonts: fonts, cmaps: cmaps,
		contents: contents, uncategorized: uncategorized, root: root}, nil
}

func (d *document) getText() (io.Reader, error) {
	var buf bytes.Buffer
	for _, pages := range d.pagesList { // get pages objects
		for _, pageRef := range pages.Kids { // get page objects
			buf.WriteString(d.getPageText(d.pageList[pageRef]))
			buf.WriteString("\n")
		}
	}
	return &buf, nil
}

func (d *document) getPageText(p *page) string {
	var buf bytes.Buffer
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
	var t textsection
	var font name
	var lastArray array
	var lastText text
	var lastName name

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
				font = lastName
			case "TJ":
				t.textArray = append(t.textArray, lastArray...)
				t.textArray = append(t.textArray, " ")
			case "T*":
				t.textArray = append(t.textArray, "\n")
			case "Tj":
				t.textArray = append(t.textArray, lastText)
			case "ET":
				t.fontName = font // use the current global text state
				sections = append(sections, t)
			}

		case array:
			lastArray = v
		case text:
			lastText = v
		case name:
			lastName = v
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
