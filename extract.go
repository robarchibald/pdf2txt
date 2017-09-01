package pdf2txt

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
)

// extract the compressed streams into text files for debugging
func extract(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}

	fileWithoutExt := path.Base(filename)[:len(path.Base(filename))-len(path.Ext(filename))]
	outDir := path.Join(path.Dir(filename), fileWithoutExt)
	os.Mkdir(outDir, 0755)

	uncategorized := make(map[string]*object)
	contents := []string{}
	fonts := make(map[string]*font)
	toUnicode := []string{}
	var decodeError error

	tchan := make(chan interface{}, 15)
	go tokenize(newBufReader(f), tchan)

	for t := range tchan {
		switch v := t.(type) {
		case error:
			return v

		case *object:
			oType := v.name("/Type")
			switch oType {
			case "/Page":
				page := v.getPage()
				contents = append(contents, page.Contents...)

			case "/Font":
				if _, ok := fonts[v.refString]; !ok {
					font := v.getFont()
					fonts[v.refString] = font
					if font.ToUnicode != "" {
						toUnicode = append(toUnicode, font.ToUnicode)
					}
				}

			case "/ObjStm":
				if decodeError != nil {
					continue
				}
				if err := v.decodeStream(); err != nil {
					decodeError = err
					continue
				}
				if err := ioutil.WriteFile(path.Join(outDir, fmt.Sprintf("objStm %s.txt", v.refString)), v.stream, 0644); err != nil {
					return err
				}
				objs, err := v.getObjectStream()
				if err != nil {
					return err
				}
				for i := range objs {
					err := ioutil.WriteFile(path.Join(outDir, fmt.Sprintf("decoded %s.txt", objs[i].refString)), []byte(fmt.Sprintf("%v", objs[i])), 0644)
					if err != nil {
						return err
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
		if err := ioutil.WriteFile(path.Join(outDir, "toUnicode "+ref+".txt"), uncategorized[ref].stream, 0644); err != nil {
			return err
		}
	}

	for i := range contents {
		ref := contents[i]
		if err := uncategorized[ref].decodeStream(); err != nil {
			return err
		}
		if err := ioutil.WriteFile(path.Join(outDir, "contents "+ref+".txt"), uncategorized[ref].stream, 0644); err != nil {
			return err
		}
	}
	return nil
}
