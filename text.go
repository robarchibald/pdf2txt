package pdf2txt

import (
	"fmt"
	"io"
)

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
			//fmt.Println("object", v)
			if v.hasTextStream() {
				schan := make(chan interface{})
				go Tokenize(newMemReader(v.stream), schan)
				count := 0
				for t := range schan {
					//					fmt.Printf("%d - %T |%v|\n", count, t, t)
					if t == nil {

					}
					count++
				}
			}
		}
	}
	return nil, nil
}
