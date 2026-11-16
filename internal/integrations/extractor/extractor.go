// Package extractor pulls plain text out of uploaded PDF CVs.
//
// Uploads are untrusted, and the PDF parser has no limits of its own: a
// small file can inflate to gigabytes, and a malformed page tree can loop.
// Text therefore checks the header, caps the page count, measures every
// stream it will decompress before parsing, and caps the text it returns.
// Isolated goes further and runs Text in a child process with a timeout and
// a memory limit, so a file that still gets past those checks can only take
// down the child.
package extractor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ledongthuc/pdf"
)

const (
	// MaxFileBytes is the largest PDF accepted.
	MaxFileBytes = 5 << 20
	// MaxPages is the most pages a CV may have.
	MaxPages = 30
	// maxDecodedBytes caps the decompressed size of all content streams and
	// font character maps together.
	maxDecodedBytes = 64 << 20
	// MaxTextBytes caps the text returned; anything longer is cut off.
	MaxTextBytes = 200 << 10
)

var (
	// ErrNotPDF means the file does not start with a PDF header.
	ErrNotPDF = errors.New("the file is not a PDF")
	// ErrTooLarge means the file is over MaxFileBytes.
	ErrTooLarge = fmt.Errorf("the file is larger than %d MB", MaxFileBytes>>20)
	// ErrTooManyPages means the PDF has more than MaxPages pages.
	ErrTooManyPages = fmt.Errorf("the PDF has more than %d pages", MaxPages)
	// ErrUnreadable means the PDF could not be parsed safely.
	ErrUnreadable = errors.New("the PDF could not be read")
)

// IsInvalidPDF reports whether err means the upload itself was rejected, as
// opposed to a failure on our side.
func IsInvalidPDF(err error) bool {
	return errors.Is(err, ErrNotPDF) || errors.Is(err, ErrTooLarge) ||
		errors.Is(err, ErrTooManyPages) || errors.Is(err, ErrUnreadable)
}

// Extract reads the PDF at fileName and returns its text. See Text.
func Extract(fileName string) (string, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return "", err
	}
	return Text(data)
}

// Text returns the plain text of the PDF in data, cut off at MaxTextBytes.
func Text(data []byte) (text string, err error) {
	if len(data) > MaxFileBytes {
		return "", ErrTooLarge
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", ErrNotPDF
	}

	// The parser panics on input it does not expect.
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("%w: %v", ErrUnreadable, r)
		}
	}()

	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreadable, err)
	}

	pageCount := r.NumPage()
	if pageCount > MaxPages {
		return "", ErrTooManyPages
	}

	pages := make([]pdf.Page, 0, pageCount)
	for i := 1; i <= pageCount; i++ {
		pages = append(pages, r.Page(i))
	}
	if err := checkDecodedSize(pages); err != nil {
		return "", err
	}

	var out strings.Builder
	fonts := make(map[string]*pdf.Font)
	for _, p := range pages {
		for _, name := range p.Fonts() {
			if _, ok := fonts[name]; !ok {
				f := p.Font(name)
				fonts[name] = &f
			}
		}
		pageText, err := p.GetPlainText(fonts)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrUnreadable, err)
		}
		out.WriteString(pageText)
		if out.Len() >= MaxTextBytes {
			break
		}
	}

	return truncate(out.String(), MaxTextBytes), nil
}

// checkDecodedSize decompresses, without keeping, every stream the text
// extraction will read, and fails once together they pass maxDecodedBytes.
func checkDecodedSize(pages []pdf.Page) error {
	budget := int64(maxDecodedBytes)
	measure := func(v pdf.Value) error {
		var streams []pdf.Value
		switch v.Kind() {
		case pdf.Stream:
			streams = []pdf.Value{v}
		case pdf.Array:
			for i := range v.Len() {
				streams = append(streams, v.Index(i))
			}
		}
		for _, s := range streams {
			if s.Kind() != pdf.Stream {
				continue
			}
			n, err := io.Copy(io.Discard, io.LimitReader(s.Reader(), budget+1))
			if err != nil {
				return fmt.Errorf("%w: %v", ErrUnreadable, err)
			}
			budget -= n
			if budget < 0 {
				return fmt.Errorf("%w: it expands to more than %d MB", ErrUnreadable, maxDecodedBytes>>20)
			}
		}
		return nil
	}

	seenFonts := make(map[string]bool)
	for _, p := range pages {
		if err := measure(p.V.Key("Contents")); err != nil {
			return err
		}
		for _, name := range p.Fonts() {
			if seenFonts[name] {
				continue
			}
			seenFonts[name] = true
			if err := measure(p.Font(name).V.Key("ToUnicode")); err != nil {
				return err
			}
		}
	}
	return nil
}

// truncate cuts s to at most n bytes without splitting a UTF-8 character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
