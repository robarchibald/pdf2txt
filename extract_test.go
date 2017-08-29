package pdf2txt

import (
	"os"
	"testing"
)

func TestExtract(t *testing.T) {
	f, _ := os.Open(`testData/Profoto.pdf`)

	extract(f)
}
