package extractor

import (
	"context"
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
	filename := "./testdata/dummy-resume.pdf"

	res, err := Extract(context.Background(), filename)
	require.NoError(t, err)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}

func TestService_ExtractFromFIle(t *testing.T) {
	filename := "./testdata/dummy-resume.pdf"
	file, err := os.Open(filename)
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() {
		require.NoError(t, file.Close())
	}()

	res, err := ExtractFromFile(context.Background(), file)
	require.NoError(t, err)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}

func TestService_ExtractStructurally(t *testing.T) {
	filename := "./testdata/dummy-resume.pdf"

	res, err := ExtractContent(t.Context(), filename)
	require.NoError(t, err)

	fmt.Printf("\n\nRes is: %s\n\n", res)
}
