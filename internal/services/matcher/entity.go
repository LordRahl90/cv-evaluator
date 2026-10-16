package matcher

// Response is the parsed LLM evaluation of a CV against a job.
type Response struct {
	// Requirements are the job's requirements with whether the CV meets
	// them and the evidence. The LLM lists them before scoring.
	Requirements []Requirement `json:"requirements"`
	OverallScore int           `json:"overall_score"`
	// Breakdown has at most one score per known criterion (see
	// entities.EvaluationCriteria), in the LLM's order. A criterion the
	// LLM did not score is absent.
	Breakdown     []CriterionScore `json:"breakdown"`
	Strengths     []string         `json:"strengths"`
	MissingSkills []string         `json:"missing_skills"`
	Summary       string           `json:"summary"`
}

// Requirement is one requirement of the job, how important it is
// (entities.RequirementMust or RequirementNice), whether the CV meets it
// (RequirementMet, RequirementPartial or RequirementNotMet) and the CV
// evidence for it.
type Requirement struct {
	Requirement string `json:"requirement"`
	Importance  string `json:"importance"`
	Met         string `json:"met"`
	Evidence    string `json:"evidence"`
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
