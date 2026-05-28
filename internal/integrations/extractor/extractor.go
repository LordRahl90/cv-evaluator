package extractor

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/ledongthuc/pdf"
)

func Extract(fileName string) (string, error) {
	f, r, err := pdf.Open(fileName)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(b); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func ExtractFromFile(file *os.File) (string, error) {
	fStat, err := file.Stat()
	if err != nil {
		return "", err
	}

	r, err := pdf.NewReader(file, fStat.Size())
	if err != nil {
		return "", err
	}

	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(b); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func ExtractContent(ctx context.Context, fileName string) (string, error) {
	f, r, err := pdf.Open(fileName)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	totalPages := r.NumPage()

	for p := 1; p <= totalPages; p++ {
		page := r.Page(p)
		if page.V.IsNull() || page.V.Key("Contents").Kind() == pdf.Null {
			// no content here
			continue
		}

		rows, err := page.GetTextByRow()
		if err != nil {
			slog.WarnContext(ctx, "failed to get text by row", "page", p, "error", err)
		}

		for _, r := range rows {
			for _, w := range r.Content {
				fmt.Printf("word: %s\n", w.S)
			}
		}

	}

	return "", nil
}
