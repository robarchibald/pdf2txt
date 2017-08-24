package pdf2txt

import (
	"fmt"
	"os"
	"testing"
)

/*func TestDecipher(t *testing.T) {
	f, _ := os.Open(`testData\page1stream.txt`)
	parse(f)
}*/

func TestTokenize(t *testing.T) {
	f, _ := os.Open(`testData/Kicker.pdf`)

	tChan := make(chan interface{})
	go Tokenize(newBufReader(f), tChan)

	count := 0
	for c := range tChan {
		fmt.Printf("%d - %T |%v|\n", count, c, c)
		count++
	}
}

func TestBinaryTokenize(t *testing.T) {
	f, _ := os.Open(`testData/250_0.txt`)

	tChan := make(chan interface{})
	go Tokenize(newBufReader(f), tChan)

	count := 0
	for c := range tChan {
		fmt.Printf("%d - %T |%v|\n", count, c, c)
		count++
	}
}

func TestTextTokenize(t *testing.T) {
	f, _ := os.Open(`testData/132_0.txt`)

	tChan := make(chan interface{})
	go Tokenize(newBufReader(f), tChan)

	count := 0
	for c := range tChan {
		fmt.Printf("%d - %T |%v|\n", count, c, c)
		count++
	}
}

func TestCmapTokenize(t *testing.T) {
	f, _ := os.Open(`testData/257_0.txt`)

	tChan := make(chan interface{})
	go Tokenize(newBufReader(f), tChan)

	count := 0
	for c := range tChan {
		fmt.Printf("%d - %T |%v|\n", count, c, c)
		count++
	}
}
