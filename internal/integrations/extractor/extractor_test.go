package extractor

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	code := 1
	defer func() {
		os.Exit(code)
	}()

	code = m.Run()
}

func TestService_Extract(t *testing.T) {
	filename := "./testdata/alugbin-abiodun-resume.pdf"

	res, err := Extract(filename)
	require.NoError(t, err)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}

func TestService_ExtractFromFIle(t *testing.T) {
	filename := "./testdata/alugbin-abiodun-resume.pdf"
	file, err := os.Open(filename)
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() {
		require.NoError(t, file.Close())
	}()

	res, err := ExtractFromFile(file)
	require.NoError(t, err)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}

func TestService_ExtractStructurally(t *testing.T) {
	filename := "./testdata/alugbin-abiodun-resume.pdf"

	res, err := ExtractContent(t.Context(), filename)
	require.NoError(t, err)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}
