package pdf2txt

import (
	"os"
	"testing"
)

/*func TestDecipher(t *testing.T) {
	f, _ := os.Open(`testData\page1stream.txt`)
	parse(f)
}*/

func TestParse(t *testing.T) {
	f, err := os.Open(`testData/stream.pdf`)
	t.Log(err)
	_, err = parsePdf(f)
	if err != nil {
		t.Fatal("unable to parse", err)
	}
}
