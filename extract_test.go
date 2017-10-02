package pdf2txt

import "testing"

func TestExtract(t *testing.T) {
	if err := extract(`testData/Profoto.pdf`); err != nil {
		t.Fatal(err)
	}

}
