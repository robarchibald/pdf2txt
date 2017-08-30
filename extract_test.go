package pdf2txt

import (
	"os"
	"testing"
)

func TestExtract(t *testing.T) {
	f, _ := os.Open(`testData/Profoto.pdf`)

	if err := extract(f); err != nil {
		t.Fatal(err)
	}
}
