package pdf2txt

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

func TestText(t *testing.T) {
	f, _ := os.Open(`testData/Kicker.pdf`)

	_, err := Text(f)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetText(t *testing.T) {
	f, _ := os.Open(`testData/132_0.txt`)

	s, err := getTextSections(newMemReader(f))
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {

	}
}

func TestProfoto(t *testing.T) {
	f, _ := os.Open(`testData/Profoto.pdf`)

	_, err := Text(f)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProfotoUG(t *testing.T) {
	f, _ := os.Open(`testData/ProfotoUserGuide.pdf`)

	r, err := Text(f)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(r.(*bytes.Buffer).String())
}
