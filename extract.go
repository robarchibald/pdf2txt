package pdf2txt

import "io"
import "io/ioutil"

import "fmt"

// extract the compressed streams into text files for debugging
func extract(r io.Reader) error {
	uncategorized := make(map[string]*object)
	contents := []string{}
	fonts := make(map[string]*font)
	toUnicode := []string{}

	tchan := make(chan interface{}, 15)
	go Tokenize(newBufReader(r), tchan)

	for t := range tchan {
		switch v := t.(type) {
		case error:
			return v

		case *object:
			oType := v.name("/Type")
			switch oType {
			case "/Page":
				page := getPage(v)
				contents = append(contents, page.Contents...)

			case "/Font":
				if _, ok := fonts[v.refString]; !ok {
					font := getFont(v)
					fonts[v.refString] = font
					if font.ToUnicode != "" {
						toUnicode = append(toUnicode, font.ToUnicode)
					}
				}

			case "/ObjStm":
				if err := ioutil.WriteFile(fmt.Sprintf("objStm %s.txt", v.refString), v.stream, 0644); err != nil {
					return err
				}
				for i := range v.values {
					if o, ok := v.values[i].(*object); ok {
						err := ioutil.WriteFile(fmt.Sprintf("decoded %s.txt", o.refString), []byte(fmt.Sprintf("%v", o)), 0644)
						if err != nil {
							return err
						}
					}
				}

			default:
				uncategorized[v.refString] = v
			}
		}
	}

	for i := range toUnicode {
		ref := toUnicode[i]
		if err := uncategorized[ref].decodeStream(); err != nil {
			return err
		}
		if err := ioutil.WriteFile("toUnicode "+ref+".txt", uncategorized[ref].stream, 0644); err != nil {
			return err
		}
	}

	for i := range contents {
		ref := contents[i]
		if err := uncategorized[ref].decodeStream(); err != nil {
			return err
		}
		if err := ioutil.WriteFile("contents "+ref+".txt", uncategorized[ref].stream, 0644); err != nil {
			return err
		}
	}
	return nil
}
