package entities

import (
	"encoding/json"
)

// Criterion is one aspect a CV is scored on against a job. Name is the
// stable key stored with each score; Guidance tells the LLM how to score it.
type Criterion struct {
	Name     string
	Guidance string
}

// EvaluationCriteria is the single list of criteria a CV is evaluated on.
// The match prompt's response schema and scoring instructions are generated
// from it, and only these criteria are kept when parsing a response. To add
// a criterion, add it here (and bump the matcher's prompt version).
var EvaluationCriteria = []Criterion{
	{
		Name:     "skills",
		Guidance: "How well the candidate's technical and soft skills cover the skills the job requires.",
	},
	{
		Name:     "experience",
		Guidance: "How relevant and deep the candidate's work experience is for the job's responsibilities.",
	},
	{
		Name: "seniority",
		Guidance: "Whether the candidate's level (years of experience, scope, leadership) matches the level " +
			"the job asks for. Over- and under-qualification both lower the score.",
	},
	{
		Name:     "domain",
		Guidance: "How familiar the candidate is with the job's industry or problem domain.",
	},
	{
		Name: "education",
		Guidance: "Whether the candidate's degrees and certifications meet the job's education requirements. " +
			"If the job states none, score by how well the education supports the role.",
	},
}

// Location is deliberately not a criterion: every search already filters by
// the location the user chose, and users may search far from where they
// live, so scoring it would mostly repeat or contradict their own choice.
// The job's listed location is still shown to the LLM as context.

// IsEvaluationCriterion reports whether name is in EvaluationCriteria.
func IsEvaluationCriterion(name string) bool {
	for _, c := range EvaluationCriteria {
		if c.Name == name {
			return true
		}
	}
	return false
}

// SimilarityHint is an embedding-based similarity between the job
// description and part of the CV, on a 0-100 scale.
type SimilarityHint struct {
	Label string
	Score float64
}

// MatchInput is everything the match prompt is rendered from.
type MatchInput struct {
	JobDescription string
	// JobLocation is the location the portal lists for the job, if any.
	// It is context only; location is not scored.
	JobLocation string
	// RelevantCV is the CV sections closest to the job description.
	RelevantCV string
	// Hints are embedding similarities, labelled by what they compare.
	Hints []SimilarityHint
	// Criteria to score. Empty means EvaluationCriteria.
	Criteria []Criterion
}

// CriteriaOrDefault returns the criteria to score.
func (in *MatchInput) CriteriaOrDefault() []Criterion {
	if len(in.Criteria) > 0 {
		return in.Criteria
	}
	return EvaluationCriteria
}

type schemaScore struct {
	Criterion string `json:"criterion"`
	Score     int    `json:"score"`
	Reason    string `json:"reason"`
}

type responseSchema struct {
	OverallScore  int           `json:"overall_score"`
	Breakdown     []schemaScore `json:"breakdown"`
	Strengths     []string      `json:"strengths"`
	MissingSkills []string      `json:"missing_skills"`
	Summary       string        `json:"summary"`
}

// ResponseSchema returns the JSON shape the LLM must answer with, with one
// breakdown entry per criterion.
func (in *MatchInput) ResponseSchema() string {
	criteria := in.CriteriaOrDefault()
	schema := responseSchema{
		Breakdown:     make([]schemaScore, len(criteria)),
		Strengths:     []string{},
		MissingSkills: []string{},
	}
	for i, c := range criteria {
		schema.Breakdown[i] = schemaScore{Criterion: c.Name}
	}
	b, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		// Marshalling this fixed struct cannot fail.
		panic(err)
	}
	return string(b)
}
