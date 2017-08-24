package pdf2txt

import (
	"os"
	"testing"
)

func TestText(t *testing.T) {
	f, _ := os.Open(`testData/Kicker.pdf`)

	Text(f)
}
