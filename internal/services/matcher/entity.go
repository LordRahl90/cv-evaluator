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

type Response struct {
	OverallScore    int      `json:"overall_score"`
	SkillsScore     int      `json:"skills_score"`
	ExperienceScore int      `json:"experience_score"`
	SeniorityScore  int      `json:"seniority_score"`
	DomainScore     int      `json:"domain_score"`
	LocationScore   int      `json:"location_score"`
	Strengths       []string `json:"strengths"`
	MissingSkills   []string `json:"missing_skills"`
	Summary         string   `json:"summary"`
}
