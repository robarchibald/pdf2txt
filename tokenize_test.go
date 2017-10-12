package pdf2txt

import (
	"io"
	"os"
	"testing"

	"github.com/EndFirstCorp/peekingReader"
)

func TestTokenize(t *testing.T) {
	f, _ := os.Open(`testData/Kicker.pdf`)

	tChan := make(chan interface{})
	go tokenize(peekingReader.NewBufReader(f), tChan)

	count := 0
	for c := range tChan {
		//fmt.Printf("%d - %T |%v|\n", count, c, c)
		if c != nil {

		}
		count++
	}
}

func TestReadDictionary(t *testing.T) {
	// dictionary string without leading << since those have been removed before readDictionary gets it
	dict := `/BleedBox[0.0 0.0 839.055 595.276]/Contents 2 0 R/CropBox[0.0 0.0 839.055 595.276]/MediaBox[0.0 0.0 839.055 595.276]/Parent 37 0 R/Resources<</ExtGState<</GS0 35 0 R/GS1 57 0 R>>/Font<</T1_0 32 0 R/T1_1 59 0 R>>/ProcSet[/PDF/Text/ImageC]/XObject<</Im0 3 0 R/Im1 4 0 R>>>>/Rotate 0/TrimBox[0.0 0.0 839.055 595.276]/Type/Page>> `
	actual, err := readDictionary(peekingReader.NewMemReader([]byte(dict)))
	if err != nil && err != io.EOF {
		t.Fatal("expected success", err)
	}

	if a, ok := actual["/BleedBox"].(array); !ok || len(a) != 4 || a[0] != token("0.0") || a[1] != token("0.0") || a[2] != token("839.055") || a[3] != token("595.276") {
		t.Error("expected valid /BleedBox", a, actual)
	}

	if c, ok := actual["/Contents"].(*objectref); !ok || c == nil || c.refString != "2 0" {
		t.Error("invalid /Contents", c)
	}

	if a, ok := actual["/CropBox"].(array); !ok || len(a) != 4 || a[0] != token("0.0") || a[1] != token("0.0") || a[2] != token("839.055") || a[3] != token("595.276") {
		t.Error("expected valid /CropBox", a)
	}

	if a, ok := actual["/MediaBox"].(array); !ok || len(a) != 4 || a[0] != token("0.0") || a[1] != token("0.0") || a[2] != token("839.055") || a[3] != token("595.276") {
		t.Error("expected valid /MediaBox", a)
	}

	if p, ok := actual["/Parent"].(*objectref); !ok || p == nil || p.refString != "37 0" {
		t.Error("expected valid /Parent", p)
	}

	r, ok := actual["/Resources"].(dictionary)
	if !ok {
		t.Error("expected valid /Resources", r)
	}

	gs, ok := r["/ExtGState"].(dictionary)
	if !ok {
		t.Error("expected valid /Resources ExtGState dictionary", gs, r)
	}
	if gs0, ok := gs["/GS0"].(*objectref); !ok || gs0.refString != "35 0" {
		t.Error("expected valid GS0 value in /Resources ExtGState dictionary", gs, gs0)
	}
	if gs1, ok := gs["/GS1"].(*objectref); !ok || gs1.refString != "57 0" {
		t.Error("expected valid GS1 value in /Resources ExtGState dictionary", gs, gs1)
	}

	font, ok := r["/Font"].(dictionary)
	if !ok {
		t.Error("expected valid /Font", font)
	}
	if t10, ok := font["/T1_0"].(*objectref); !ok || t10.refString != "32 0" {
		t.Error("expected valid T1_0 value", t10)
	}
	if t11, ok := font["/T1_1"].(*objectref); !ok || t11.refString != "59 0" {
		t.Error("expected valid T1_1 value", t11)
	}

	if ps, ok := r["/ProcSet"].(array); !ok || len(ps) != 3 || ps[0] != name("/PDF") || ps[1] != name("/Text") || ps[2] != name("/ImageC") {
		t.Error("expected valid /ProcSet")
	}

	xo, ok := r["/XObject"].(dictionary)
	if !ok {
		t.Error("expected valid /XObject", font)
	}
	if im0, ok := xo["/Im0"].(*objectref); !ok || im0.refString != "3 0" {
		t.Error("expected valid Im0 value", im0)
	}
	if im1, ok := xo["/Im1"].(*objectref); !ok || im1.refString != "4 0" {
		t.Error("expected valid Im1 value", im1)
	}

	if rot, ok := actual["/Rotate"].(token); !ok || rot != "0" {
		t.Error("expected valid /Rotate", rot)
	}

	if tb, ok := actual["/TrimBox"].(array); !ok || len(tb) != 4 || tb[0] != token("0.0") || tb[1] != token("0.0") || tb[2] != token("839.055") || tb[3] != token("595.276") {
		t.Error("expected valid /TrimBox", tb)
	}

	if tp, ok := actual["/Type"].(name); !ok || tp != "/Page" {
		t.Error("expected valid /Type", tp)
	}

	// check for empty value
	dict = `/Empty>> `
	actual, err = readDictionary(peekingReader.NewMemReader([]byte(dict)))

	if c, ok := actual["/Empty"].(null); !ok || c != null(true) {
		t.Error("expected valid /Empty")
	}

	// error on readName
	dict = `/Empty`
	actual, err = readDictionary(peekingReader.NewMemReader([]byte(dict)))
	if err != io.EOF {
		t.Error("expected error")
	}

	// error on readNext
	dict = `/Empty 5  0`
	actual, err = readDictionary(peekingReader.NewMemReader([]byte(dict)))
	if err != io.EOF {
		t.Error("expected error")
	}

	// error on Peek
	dict = `/Empty 5  `
	actual, err = readDictionary(peekingReader.NewMemReader([]byte(dict)))
	if err != io.EOF {
		t.Error("expected error")
	}

	// check to make sure it works with spaces between names
	dict = `/Type /Catalog
/Outlines 2 0 R
/Pages 6 0 R
>> `
	actual, _ = readDictionary(peekingReader.NewMemReader([]byte(dict)))
	if actual == nil || actual["/Type"] != name("/Catalog") {
		t.Error("expected valid dictionary", actual)
	}
	if o, ok := actual["/Outlines"].(*objectref); !ok || o.refString != "2 0" {
		t.Error("expected valid refString", o)
	}
	if o, ok := actual["/Pages"].(*objectref); !ok || o.refString != "6 0" {
		t.Error("expected valid refString", o)
	}
}

func TestTextTokenize(t *testing.T) {
	f, _ := os.Open(`testData/132_0.txt`)

	tChan := make(chan interface{})
	go tokenize(peekingReader.NewBufReader(f), tChan)

	count := 0
	for c := range tChan {
		//fmt.Printf("%d - %T |%v|\n", count, c, c)
		if c != nil {

		}
		count++
	}
}

func TestCmapTokenize(t *testing.T) {
	f, _ := os.Open(`testData/257_0.txt`)

	tChan := make(chan interface{})
	go tokenize(peekingReader.NewBufReader(f), tChan)

	count := 0
	for c := range tChan {
		//fmt.Printf("%d - %T |%v|\n", count, c, c)
		if c != nil {

		}
		count++
	}
}

func TestProfotoTokenize(t *testing.T) {
	f, _ := os.Open(`testData/Profoto.pdf`)

	tChan := make(chan interface{})
	go tokenize(peekingReader.NewBufReader(f), tChan)

	count := 0
	for c := range tChan {
		//fmt.Printf("%d - %T |%v|\n", count, c, c)
		if c != nil {

		}
		count++
	}
}

func TestProfotoUGTokenize(t *testing.T) {
	f, _ := os.Open(`testData/ProfotoUserGuide.pdf`)

	tChan := make(chan interface{})
	go tokenize(peekingReader.NewBufReader(f), tChan)

	count := 0
	for c := range tChan {
		//fmt.Printf("%d - %T |%v|\n", count, c, c)
		if c != nil {

		}
		count++
	}
}

func TestCatalogTokenize(t *testing.T) {
	f, _ := os.Open(`testData/catalog.txt`)
	tChan := make(chan interface{})
	go tokenize(peekingReader.NewBufReader(f), tChan)
	tok := <-tChan
	if obj, ok := tok.(*object); !ok || obj.refString != "7967 0" || obj.dict["/Type"] != name("/Catalog") {
		t.Error("unable to parse", obj)
	}
}
