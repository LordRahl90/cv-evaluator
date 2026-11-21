//go:build eval

// Embedding similarity against the prompt evaluation cases, with the real
// OpenAI embeddings API. Run it with `make eval-similarity` (needs
// OPENAI_API_KEY). It embeds each case's CV and job and prints their cosine
// similarity next to the score band the case expects, to judge how well
// similarity alone ranks jobs before choosing WORKER_SIMILARITY_FLOOR. It
// asserts nothing beyond the requests succeeding.
package matcher

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/llm/openai"

	"golang.org/x/sync/errgroup"
)

func TestSimilarityEval(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}

	b, err := os.ReadFile("testdata/prompt_eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		CVs   map[string]string `json:"cvs"`
		Cases []evalCase        `json:"cases"`
	}
	if err := json.Unmarshal(b, &suite); err != nil {
		t.Fatal(err)
	}

	embedder := openai.New(&http.Client{Timeout: time.Minute}, key, "")

	// Every distinct text is embedded once, a few at a time.
	texts := map[string][]float32{}
	for _, cv := range suite.CVs {
		texts[cv] = nil
	}
	for _, c := range suite.Cases {
		texts[c.Job] = nil
	}
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(4)
	for _, text := range slices.Collect(maps.Keys(texts)) {
		g.Go(func() error {
			v, err := embedder.CreateEmbedding(ctx, text)
			if err != nil {
				return err
			}
			mu.Lock()
			texts[text] = v
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("embed: %v", err)
	}

	type row struct {
		name       string
		min, max   int
		similarity float64
	}
	rows := make([]row, 0, len(suite.Cases))
	for _, c := range suite.Cases {
		cv, ok := suite.CVs[c.CV]
		if !ok {
			t.Fatalf("case %q: unknown cv %q", c.Name, c.CV)
		}
		rows = append(rows, row{name: c.Name, min: c.Min, max: c.Max, similarity: cosine(texts[cv], texts[c.Job])})
	}
	slices.SortFunc(rows, func(a, b row) int { return cmp.Compare(b.similarity, a.similarity) })

	var report strings.Builder
	fmt.Fprintf(&report, "\n%-48s %-10s %s\n", "case", "similarity", "band")
	for _, r := range rows {
		fmt.Fprintf(&report, "%-48s %-10.4f %d-%d\n", r.name, r.similarity, r.min, r.max)
	}

	// Rank agreement: of the pairs of cases whose bands do not overlap, the
	// share where the case with the higher band also has the higher
	// similarity. 1 means similarity orders them exactly as the bands do,
	// 0.5 is chance.
	var pairs, agree int
	for i := range rows {
		for j := i + 1; j < len(rows); j++ {
			a, b := rows[i], rows[j]
			if a.min <= b.max && b.min <= a.max {
				continue
			}
			pairs++
			// rows is sorted by similarity, so a is at least as similar as b.
			if a.min > b.max && a.similarity > b.similarity {
				agree++
			}
		}
	}
	if pairs > 0 {
		fmt.Fprintf(&report, "\nrank agreement: %d of %d pairs with separate bands (%.0f%%)\n",
			agree, pairs, 100*float64(agree)/float64(pairs))
	}
	t.Logf("%s, %d cases%s", openai.EmbeddingModel, len(rows), report.String())
}

// cosine returns the cosine similarity of a and b, the same value as
// 1 - (a <=> b) in pgvector; 0 when either is a zero vector.
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
