package pdf2txt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

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
	catalogs := make(map[string]*catalog)
	pagesList := make(map[string]*pages)
	pageList := make(map[string]*page)
	fonts := make(map[string]*font)
	cmaps := make(map[string]cmap)
	contents := make(map[string][]textsection)
	uncategorized := make(map[string]*object)

	tchan := make(chan interface{}, 15)
	go Tokenize(newBufReader(r), tchan)

	for t := range tchan {
		switch v := t.(type) {
		case text:
			fmt.Println("text", v)
		case array:
			fmt.Println("array", v)
		case hexdata:
			fmt.Println("hexdata", v)
		case *object:
			oType := v.name("/Type")
			switch oType {
			case "/Catalog":
				catalogs[v.refString] = &catalog{v.objectref("/Pages").refString}

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
	for i := range catalogs { // loop through catalogs to find all pages
		catalog := catalogs[i]
		if _, ok := pagesList[catalog.Pages]; !ok { // create Pages object if not created yet
			if pages, ok := uncategorized[catalog.Pages]; ok {
				pagesList[catalog.Pages] = getPages(pages)
				delete(uncategorized, catalog.Pages)
			}
		}
		pagesObj := pagesList[catalog.Pages]
		var kids []string
		if pagesObj != nil {
			kids = pagesObj.Kids
		} else {
			for ref := range pageList {
				if pageList[ref].Parent == catalog.Pages {
					kids = append(kids, ref)
				}
			}
		}
		for pCount := range kids { // loop through pages
			pageRef := kids[pCount]
			if pageList[pageRef] == nil {
				page := getPage(uncategorized[pageRef])
				pageList[pageRef] = page
				delete(uncategorized, pageRef)
			}
			page := pageList[pageRef]
			contentsRefs := page.Contents
			for cIndex := range page.Contents {
				if c, ok := contents[contentsRefs[cIndex]]; !ok || c == nil {
					c, err := getTextSections(newMemReader(uncategorized[contentsRefs[cIndex]].stream))
					if err != nil {
						return nil, err
					}
					contents[contentsRefs[cIndex]] = c
					delete(uncategorized, contentsRefs[cIndex])
				}
			}
			for name := range page.Fonts {
				fontRef := page.Fonts[name]
				if _, ok := fonts[fontRef]; !ok {
					if uncategorized[fontRef] == nil {
						continue
					}
					fonts[fontRef] = getFont(uncategorized[fontRef])
					delete(uncategorized, fontRef)
				}
				font := fonts[fontRef]
				cmapRef := font.ToUnicode
				if cmap, ok := cmaps[cmapRef]; (!ok || cmap == nil) && cmapRef != "" {
					cmap, err := getCmap(newMemReader(uncategorized[cmapRef].stream))
					if err != nil {
						return nil, err
					}
					cmaps[cmapRef] = cmap
					delete(uncategorized, cmapRef)
				}
			}
		}
	}

	var buf bytes.Buffer
	for key := range pagesList {
		v := pagesList[key]
		for i := range v.Kids {
			pref := v.Kids[i]
			page := pageList[pref]
			for cIndex := range page.Contents {
				cref := page.Contents[cIndex]
				c := contents[cref]
				for contentIndex := range c {
					section := c[contentIndex]
					buf.WriteString(string(section.text))
					for ai := range section.textArray {
						item := section.textArray[ai]
						switch t := item.(type) {
						case hexdata:
							font := fonts[page.Fonts[section.fontName]]
							var cmap map[hexdata]string
							if font != nil && font.ToUnicode != "" && cmaps[font.ToUnicode] != nil {
								cmap = cmaps[font.ToUnicode]
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
	}
	//fmt.Println("catalogs", catalogs)
	//fmt.Println("pagesList", pagesList)
	//fmt.Println("pageList", pageList)
	//fmt.Println("fonts", fonts)
	//fmt.Println("contents", contents)
	//fmt.Println("cmaps", cmaps)
	//fmt.Println("uncategorized", uncategorized)
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
