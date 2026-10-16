package deepseek

import (
	"encoding/json"
	"strings"
	"testing"

	"cv-evaluator/internal/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPrompt_GeneratesSchemaFromCriteria(t *testing.T) {
	svc := New(nil)
	prompt, err := svc.buildPrompt(&entities.MatchInput{
		JobDescription: "Senior Go engineer, hybrid in Copenhagen",
		JobLocation:    "Copenhagen, Denmark",
		RelevantCV:     "- Go, Kubernetes",
		Hints: []entities.SimilarityHint{
			{Label: "Title similarity", Score: 81.4},
			{Label: "Skills section similarity", Score: 66},
		},
	})
	require.NoError(t, err)

	// The schema is plain JSON (not HTML-escaped) with one entry per criterion.
	start := strings.Index(prompt, "{")
	end := strings.Index(prompt, "\n}") + 2
	var schema struct {
		OverallScore int `json:"overall_score"`
		Breakdown    []struct {
			Criterion string `json:"criterion"`
		} `json:"breakdown"`
	}
	require.NoError(t, json.Unmarshal([]byte(prompt[start:end]), &schema), prompt[start:end])
	var names []string
	for _, b := range schema.Breakdown {
		names = append(names, b.Criterion)
	}
	assert.Equal(t, []string{"skills", "experience", "seniority", "domain", "education"}, names)

	for _, c := range entities.EvaluationCriteria {
		assert.Contains(t, prompt, "- "+c.Name+": "+c.Guidance)
	}
	assert.Contains(t, prompt, "- Title similarity: 81")
	assert.Contains(t, prompt, "- Skills section similarity: 66")
	assert.Contains(t, prompt, "JOB LOCATION (as listed by the portal; context only, not scored): Copenhagen, Denmark")
	assert.Contains(t, prompt, "Do not score or penalise the job's location")
	assert.NotContains(t, prompt, "- location:")
	assert.NotContains(t, prompt, "Don't consider the location")
	assert.NotContains(t, prompt, "Seniority:", "seniority is no longer fed the title similarity")
	assert.NotContains(t, prompt, "&#34;", "the prompt must not be HTML-escaped")
}

func TestBuildPrompt_CustomCriteriaAndNoHints(t *testing.T) {
	svc := New(nil)
	prompt, err := svc.buildPrompt(&entities.MatchInput{
		JobDescription: "x",
		Criteria:       []entities.Criterion{{Name: "culture", Guidance: "Team fit."}},
	})
	require.NoError(t, err)
	assert.Contains(t, prompt, `"criterion": "culture"`)
	assert.NotContains(t, prompt, `"criterion": "skills"`)
	assert.Contains(t, prompt, "- none available")
	assert.Contains(t, prompt, "context only, not scored): not listed")
}
