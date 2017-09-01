package pdf2txt

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestText(t *testing.T) {
	f, _ := os.Open(`testData/Kicker.pdf`)

	_, err := Text(f)
	if err != nil {
		t.Fatal(err)
	}
	//fmt.Println(r.(*bytes.Buffer).String())
}

func TestGetText(t *testing.T) {
	b, _ := ioutil.ReadFile(`testData/132_0.txt`)

	s, err := getTextSections(newMemReader(b))
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
	//fmt.Println(r.(*bytes.Buffer).String())
}

func TestGetTextSections(t *testing.T) {
	b, _ := ioutil.ReadFile(`testData/textSection.txt`)
	_, err := getTextSections(newMemReader(b))
	if err != nil {
		t.Fatal(err)
	}
}

func TestProfotoUG(t *testing.T) {
	f, _ := os.Open(`testData/ProfotoUserGuide.pdf`)

	_, err := Text(f)
	if err != nil {
		t.Fatal(err)
	}
	//fmt.Println(r.(*bytes.Buffer).String())
}

func TestGetObjectStream(t *testing.T) {
	b, _ := ioutil.ReadFile(`testData/objectstream.txt`)
	o := &object{dict: dictionary{"/Type": name("/ObjStm"), "/N": token("5"), "/First": token("34")}}
	o.stream = b
	_, err := o.getObjectStream()
	if err != nil {
		t.Fatal(err)
	}
}
