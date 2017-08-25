package pdf2txt

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type catalog struct {
	pages objectref
}

type pages struct {
	page
}

type page struct {
}

// Text extracts text from an io.Reader stream of a PDF file
// and outputs it into a new io.Reader filled with the text
// contained in the PDF file.
func Text(r io.Reader) (io.Reader, error) {
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
			if objref := v.pages(); objref != nil {
				fmt.Println("objref", objref)
			}
			//			fmt.Println("object", v)

			if v.hasTextStream() {
				schan := make(chan interface{})
				go textTokenize(newMemReader(v.stream), schan)
				count := 0
				for t := range schan {
					fmt.Printf("%d - %T |%v|\n", count, t, t)
					if t == nil {

					}
					count++
				}
			}
		}
	}
	return nil, nil
}

func textTokenize(r peekingReader, tChan chan interface{}) {
	var err error

Loop:
	for {
		item := readNext(r)
		fmt.Println(item)

		switch v := item.(type) {
		case error:
			err = v
			break Loop

		case token:
			switch v {
			case "BT":
				var textsection *textsection
				textsection, err = readTextSection(r)
				if err != nil {
					break Loop
				}
				tChan <- textsection
			case "begincmap":
				var cmap cmap
				cmap, err = readCmap(r)
				if err != nil {
					break Loop
				}
				tChan <- cmap
			}
		}
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
