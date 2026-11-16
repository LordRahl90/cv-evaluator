package extractor

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain lets the test binary act as the extraction child, the way the
// API binary does, so Isolated can be tested end to end.
func TestMain(m *testing.M) {
	RunChildIfRequested()
	os.Exit(m.Run())
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("./testdata/dummy-resume.pdf")
	require.NoError(t, err)
	return data
}

func TestText_ExtractsARealCV(t *testing.T) {
	text, err := Text(readFixture(t))
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(text))
}

func TestExtract_ReadsAFile(t *testing.T) {
	text, err := Extract("./testdata/dummy-resume.pdf")
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(text))
}

func TestText_RejectsHostileFiles(t *testing.T) {
	tests := map[string]struct {
		data []byte
		want error
	}{
		"not a pdf":            {data: []byte("PK\x03\x04 a zip file"), want: ErrNotPDF},
		"too large":            {data: append([]byte("%PDF-1.4\n"), make([]byte, MaxFileBytes)...), want: ErrTooLarge},
		"too many pages":       {data: buildPDF(t, MaxPages+1, []byte("BT ET")), want: ErrTooManyPages},
		"compression bomb":     {data: buildPDF(t, 1, bytes.Repeat([]byte(" "), maxDecodedBytes+1)), want: ErrUnreadable},
		"garbage after header": {data: []byte("%PDF-1.4\n\x00\x01garbage"), want: ErrUnreadable},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Text(tt.data)
			require.ErrorIs(t, err, tt.want)
			assert.True(t, IsInvalidPDF(err))
		})
	}
}

func TestText_CapsTheText(t *testing.T) {
	line := "(" + strings.Repeat("a", 1000) + ") Tj\n"
	content := []byte("BT /F1 12 Tf " + strings.Repeat(line, MaxTextBytes/1000+10) + "ET")

	text, err := Text(buildPDF(t, 1, content))
	require.NoError(t, err)
	assert.LessOrEqual(t, len(text), MaxTextBytes)
	assert.Greater(t, len(text), MaxTextBytes/2, "the fixture must produce enough text to hit the cap")
}

func TestIsolated_ExtractsInAChildProcess(t *testing.T) {
	x, err := NewIsolated()
	require.NoError(t, err)

	text, err := x.Text(t.Context(), readFixture(t))
	require.NoError(t, err)
	want, err := Text(readFixture(t))
	require.NoError(t, err)
	assert.Equal(t, want, text)
}

func TestIsolated_ReportsRejections(t *testing.T) {
	x, err := NewIsolated()
	require.NoError(t, err)

	_, err = x.Text(t.Context(), buildPDF(t, MaxPages+1, []byte("BT ET")))
	require.ErrorIs(t, err, ErrTooManyPages)

	_, err = x.Text(t.Context(), buildPDF(t, 1, bytes.Repeat([]byte(" "), maxDecodedBytes+1)))
	require.ErrorIs(t, err, ErrUnreadable)

	_, err = x.Text(t.Context(), []byte("not a pdf"))
	require.ErrorIs(t, err, ErrNotPDF)
}

// buildPDF writes a minimal PDF with the given number of pages, each showing
// content compressed with Flate.
func buildPDF(t *testing.T, pages int, content []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer
	zw, err := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	require.NoError(t, err)
	_, err = zw.Write(content)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	// Objects: 1 catalog, 2 page tree, 3 font, 4 content, then one per page.
	var kids []string
	for i := range pages {
		kids = append(kids, fmt.Sprintf("%d 0 R", 5+i))
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", compressed.Len(), compressed.Bytes()),
	}
	for range pages {
		objects = append(objects, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 4 0 R >>")
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}
