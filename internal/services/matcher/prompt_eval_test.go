//go:build eval

// Prompt evaluation against the real DeepSeek API. It is excluded from
// normal test runs by the eval build tag; run it with `make eval-prompt`
// (needs DEEPSEEK_API_KEY). Each case is a CV and a job with the overall
// score band a good answer should land in, so prompt changes can be
// compared by running this before and after.
package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/models"

	"golang.org/x/sync/errgroup"
)

type evalCase struct {
	Name              string `json:"name"`
	CV                string `json:"cv"`
	Location          string `json:"location"`
	Job               string `json:"job"`
	Min               int    `json:"min"`
	Max               int    `json:"max"`
	ExpectMissingMust bool   `json:"expect_missing_must"`
	ExpectPartialMust bool   `json:"expect_partial_must"`
	ExpectEnglish     bool   `json:"expect_english"`
}

type evalRun struct {
	resp *Response
	err  error
}

func TestPromptEval(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY is not set")
	}
	runs := 3
	if v, err := strconv.Atoi(os.Getenv("EVAL_RUNS")); err == nil && v > 0 {
		runs = v
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

	svc := New(nil, deepseek.New(deepseek.NewClient(key)))

	// Every case runs `runs` times, a few requests at a time.
	results := make([][]evalRun, len(suite.Cases))
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(4)
	for i, c := range suite.Cases {
		cv, ok := suite.CVs[c.CV]
		if !ok {
			t.Fatalf("case %q: unknown cv %q", c.Name, c.CV)
		}
		for range runs {
			g.Go(func() error {
				reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				resp, _, err := svc.evaluate(reqCtx, cv, &models.Job{Detail: c.Job, Location: c.Location})
				mu.Lock()
				results[i] = append(results[i], evalRun{resp: resp, err: err})
				mu.Unlock()
				return nil
			})
		}
	}
	_ = g.Wait()

	var report strings.Builder
	fmt.Fprintf(&report, "\n%-48s %-9s %-14s %s\n", "case", "band", "overall", "spread")
	for i, c := range suite.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var scores []int
			for _, r := range results[i] {
				if r.err != nil {
					t.Errorf("evaluation failed: %v", r.err)
					continue
				}
				scores = append(scores, r.resp.OverallScore)
				if r.resp.OverallScore < c.Min || r.resp.OverallScore > c.Max {
					t.Errorf("overall %d outside %d-%d; summary: %s", r.resp.OverallScore, c.Min, c.Max, r.resp.Summary)
				}
				if c.ExpectMissingMust && !hasMissingMust(r.resp) {
					t.Errorf("expected at least one must-have marked \"no\"; requirements: %+v", r.resp.Requirements)
				}
				if c.ExpectPartialMust && !hasMust(r.resp, entities.RequirementPartial) {
					t.Errorf("expected at least one must-have marked \"partial\"; requirements: %+v", r.resp.Requirements)
				}
				if c.ExpectEnglish && !looksEnglish(r.resp.Summary) {
					t.Errorf("summary is not in English: %s", r.resp.Summary)
				}
				if len(r.resp.Breakdown) == 0 {
					t.Errorf("no criteria scored")
				}
			}
			spread := 0
			if len(scores) > 0 {
				spread = slices.Max(scores) - slices.Min(scores)
				if spread > 15 {
					t.Logf("unstable: overall scores %v vary by %d across runs", scores, spread)
				}
			}
			fmt.Fprintf(&report, "%-48s %3d-%-5d %-14s %d\n", c.Name, c.Min, c.Max, fmt.Sprint(scores), spread)
		})
	}
	t.Logf("prompt %s, %d runs per case%s", "jobmatch.tpl", runs, report.String())
}

func hasMissingMust(r *Response) bool {
	return hasMust(r, entities.RequirementNotMet)
}

func hasMust(r *Response, met string) bool {
	for _, req := range r.Requirements {
		if req.Importance == entities.RequirementMust && req.Met == met {
			return true
		}
	}
	return false
}

// looksEnglish is a cheap check that the summary is English and addressed
// to the candidate, as the prompt asks.
func looksEnglish(s string) bool {
	lower := " " + strings.ToLower(s) + " "
	if !strings.Contains(lower, " you") {
		return false
	}
	for _, danish := range []string{" og ", " erfaring", " med ", " ikke "} {
		if strings.Contains(lower, danish) {
			return false
		}
	}
	return true
}
