package pdf2txt

import (
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
	Parent   string
	Fonts    map[name]string
	Contents string
}

type font struct {
	Encoding  name
	ToUnicode string
}

type content struct {
	textSections []textsection
}

// Text extracts text from an io.Reader stream of a PDF file
// and outputs it into a new io.Reader filled with the text
// contained in the PDF file.
func Text(r io.Reader) (io.Reader, error) {
	catalogs := make(map[string]catalog)
	pagesList := make(map[string]pages)
	pageList := make(map[string]page)
	fonts := make(map[string]font)
	contents := make(map[string]content)
	uncategorized := make(map[string]object)

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
				catalogs[v.refString()] = catalog{v.objectref("/Pages").refString()}

			case "/Pages":
				p := v.array("/Kids")
				kids := make([]string, len(p))
				for i := range p {
					if oref, ok := p[i].(*objectref); ok {
						kids[i] = oref.refString()
					}
				}
				pagesList[v.refString()] = pages{Kids: kids, Count: v.int("/Count")}

			case "/Page":
				page := page{Fonts: make(map[name]string)}
				if res, ok := v.search("/Resources").(dictionary); ok {
					if fonts, ok := res["/Font"].(dictionary); ok {
						for key, value := range fonts {
							fmt.Println("font keys & values", key, value)
							if oref, ok := value.(*objectref); ok {
								page.Fonts[key] = oref.refString()
							}
						}
					}
				}
				if p := v.objectref("/Parent"); p != nil {
					page.Parent = p.refString()
				}
				if c := v.objectref("/Contents"); c != nil {
					refString := c.refString()
					page.Contents = refString
					if _, ok := contents[refString]; !ok {
						contents[refString] = content{}
					}
				}
				pageList[v.refString()] = page

			case "/Font":
				font := font{Encoding: v.name("/Encoding")}
				if u := v.objectref("/ToUnicode"); u != nil {
					font.ToUnicode = u.refString()
				}
				fonts[v.refString()] = font

			case "/XObject": // we don't need
			case "/FontDescriptor": // we don't need
			default:
				refString := v.refString()
				if _, ok := contents[refString]; ok { // something has already referenced this as content so save as content
					sections, err := getTextSections(newMemReader(v.stream))
					if err != nil {
						fmt.Println("error getting textsection", err)
						return nil, err // maybe I shouldn't error completely if one page is bad
					}
					contents[refString] = content{textSections: sections}
				} else {
					uncategorized[refString] = *v
				}
			}
		}
	}
	fmt.Println("catalogs", catalogs)
	fmt.Println("pagesList", pagesList)
	fmt.Println("pageList", pageList)
	fmt.Println("fonts", fonts)
	fmt.Println("contents", contents)
	fmt.Println("uncategorized", uncategorized)
	return nil, nil
}

func getTextSections(r peekingReader) ([]textsection, error) {
	sections := []textsection{}
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
				textsection, err := readTextSection(r)
				if err != nil {
					return nil, err
				}
				sections = append(sections, *textsection)
			}
		}
	}
}

func getCmap(r peekingReader) (cmap, error) {
	for {
		item := readNext(r)

		switch v := item.(type) {
		case error:
			if v != io.EOF {
				return nil, v
			}

		case token:
			switch v {
			case "begincmap":
				cmap, err := readCmap(r)
				if err != nil {
					return nil, err
				}
				return cmap, nil
			}
		}
	}
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
					t.fontName = name
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
	var prev token

	for {
		item := readNext(r)
		switch v := item.(type) {
		case error:
			return nil, v

		case token:
			switch v {
			case "begincodespacerange":
				length, _ := strconv.Atoi(string(prev))
				for i := 0; i < length*2; i++ {

				}
			case "beginbfchar":
				cmap, err := readbfchar(r, prev)
				if err != nil {
					return nil, err
				}
				if cmap != nil {

				}
			case "beginbfrange":
				cmap, err := readbfrange(r, prev)
				if err != nil {
					return nil, err
				}
				if cmap != nil {

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
