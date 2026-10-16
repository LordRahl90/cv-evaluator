//go:build eval

// Cover letter prompt evaluation against the real DeepSeek API, excluded
// from normal test runs by the eval build tag; run it with
// `make eval-prompt` (needs DEEPSEEK_API_KEY). Each case is a CV from the
// match suite and a job; a good letter passes every server-side check
// (first time or after the one revision), uses facts from the CV and
// ignores instructions hidden in the job description.
package coverletter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/services/matcher"

	"golang.org/x/sync/errgroup"
)

type letterEvalCase struct {
	Name           string            `json:"name"`
	CV             string            `json:"cv"`
	Candidate      string            `json:"candidate"`
	Job            string            `json:"job"`
	Evaluation     *matcher.Response `json:"evaluation"`
	ExpectFacts    []string          `json:"expect_facts"`
	MinFacts       int               `json:"min_facts"`
	ExpectMentions []string          `json:"expect_mentions"`
	Forbid         []string          `json:"forbid"`
}

type letterEvalRun struct {
	letter *Letter
	err    error
}

func TestCoverLetterEval(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY is not set")
	}
	runs := 3
	if v, err := strconv.Atoi(os.Getenv("EVAL_RUNS")); err == nil && v > 0 {
		runs = v
	}

	var cvs struct {
		CVs map[string]string `json:"cvs"`
	}
	readJSON(t, "../matcher/testdata/prompt_eval.json", &cvs)
	var suite struct {
		Cases []letterEvalCase `json:"cases"`
	}
	readJSON(t, "testdata/cover_letter_eval.json", &suite)

	svc := New(nil, nil, deepseek.New(deepseek.NewClient(key)))

	results := make([][]letterEvalRun, len(suite.Cases))
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(4)
	for i, c := range suite.Cases {
		cv, ok := cvs.CVs[c.CV]
		if !ok {
			t.Fatalf("case %q: unknown cv %q", c.Name, c.CV)
		}
		for range runs {
			g.Go(func() error {
				reqCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
				defer cancel()
				letter, err := svc.Write(reqCtx, Input{
					Candidate:      Candidate{Name: c.Candidate},
					CV:             cv,
					JobDescription: c.Job,
					Evaluation:     c.Evaluation,
				})
				mu.Lock()
				results[i] = append(results[i], letterEvalRun{letter: letter, err: err})
				mu.Unlock()
				return nil
			})
		}
	}
	_ = g.Wait()

	var report strings.Builder
	fmt.Fprintf(&report, "\n%-44s %-8s %s\n", "case", "passed", "words")
	for i, c := range suite.Cases {
		t.Run(c.Name, func(t *testing.T) {
			passed := 0
			var words []int
			for _, r := range results[i] {
				if r.err != nil {
					t.Errorf("letter failed: %v", r.err)
					continue
				}
				l := r.letter
				ok := true
				fail := func(format string, args ...any) {
					ok = false
					t.Errorf(format+"\n---\n%s\n%s", append(args, l.Subject, l.Letter)...)
				}
				if v := Check(*l, Candidate{Name: c.Candidate}); len(v) > 0 {
					fail("checks failed: %v", Names(v))
				}
				lower := strings.ToLower(l.Subject + "\n" + l.Letter)
				facts := 0
				for _, f := range c.ExpectFacts {
					if strings.Contains(lower, strings.ToLower(f)) {
						facts++
					}
				}
				if facts < c.MinFacts {
					fail("only %d of the CV facts %v appear, want %d", facts, c.ExpectFacts, c.MinFacts)
				}
				for _, m := range c.ExpectMentions {
					if !strings.Contains(lower, strings.ToLower(m)) {
						fail("letter does not mention %q", m)
					}
				}
				for _, f := range c.Forbid {
					if containsWord(lower, strings.ToLower(f)) {
						fail("letter contains %q", f)
					}
				}
				if l.Letter == strings.ToUpper(l.Letter) {
					fail("letter is in capital letters")
				}
				if ok {
					passed++
				}
				if os.Getenv("EVAL_SHOW") != "" {
					t.Logf("%d words\n%s\n\n%s", l.WordCount, l.Subject, l.Letter)
				}
				words = append(words, l.WordCount)
			}
			fmt.Fprintf(&report, "%-44s %d/%-6d %v\n", c.Name, passed, runs, words)
		})
	}
	t.Logf("prompt %s, %d runs per case%s", "coverletter.tpl", runs, report.String())
}

// containsWord reports whether s contains word, not as part of a longer
// word ("yo" does not match "your").
func containsWord(s, word string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], word)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(word)
		if (start == 0 || !isWordByte(s[start-1])) && (end == len(s) || !isWordByte(s[end])) {
			return true
		}
		i = start + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}
