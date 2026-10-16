package extractor

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/ledongthuc/pdf"

	"cv-evaluator/internal/telemetry"
)

// Extract returns the plain text of the PDF at fileName.
// The PDF library has no spans of its own, so its failures are recorded
// here, where they happen.
func Extract(ctx context.Context, fileName string) (string, error) {
	_, span := telemetry.Start(ctx, "extractor.Extract")
	defer span.End()

	f, r, err := pdf.Open(fileName)
	if err != nil {
		return "", span.RecordError(err)
	}
	defer func() {
		_ = f.Close()
	}()

	b, err := r.GetPlainText()
	if err != nil {
		return "", span.RecordError(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(b); err != nil {
		return "", span.RecordError(err)
	}
	return buf.String(), nil
}

// ExtractFromFile returns the plain text of the PDF in file.
// Failures are recorded here, like Extract's.
func ExtractFromFile(ctx context.Context, file *os.File) (string, error) {
	_, span := telemetry.Start(ctx, "extractor.ExtractFromFile")
	defer span.End()

	fStat, err := file.Stat()
	if err != nil {
		return "", span.RecordError(err)
	}

	r, err := pdf.NewReader(file, fStat.Size())
	if err != nil {
		return "", span.RecordError(err)
	}

	b, err := r.GetPlainText()
	if err != nil {
		return "", span.RecordError(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(b); err != nil {
		return "", span.RecordError(err)
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
