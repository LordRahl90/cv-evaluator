package matcher

// MatchResult holds a CV section alongside its similarity score (0–1) and
// raw cosine distance (0–2) relative to the query embedding.
// Score = 1 - Distance; higher score means a closer match.
type MatchResult struct {
	SectionHeading string  `gorm:"column:section_heading" json:"section_heading"`
	Content        string  `gorm:"column:section"         json:"content"`
	Distance       float32 `gorm:"column:distance"        json:"distance"`
	Score          float32 `gorm:"column:score"           json:"score"`
}

type CVContext struct {
	Skills     []MatchResult
	Experience []MatchResult
	Education  []MatchResult
	Title      []MatchResult
	Summary    []MatchResult
	Other      []MatchResult
}

// Response is the parsed LLM evaluation of a CV against a job.
type Response struct {
	OverallScore int `json:"overall_score"`
	// Breakdown has at most one score per known criterion (see
	// entities.EvaluationCriteria), in the LLM's order. A criterion the
	// LLM did not score is absent.
	Breakdown     []CriterionScore `json:"breakdown"`
	Strengths     []string         `json:"strengths"`
	MissingSkills []string         `json:"missing_skills"`
	Summary       string           `json:"summary"`
}

// CriterionScore is one criterion's score (0-100) and the reason for it.
type CriterionScore struct {
	Criterion string `json:"criterion"`
	Score     int    `json:"score"`
	Reason    string `json:"reason"`
}

// Score returns the score for criterion and whether it was scored.
func (r *Response) Score(criterion string) (int, bool) {
	for _, s := range r.Breakdown {
		if s.Criterion == criterion {
			return s.Score, true
		}
	}
	return 0, false
}
