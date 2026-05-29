package entities

type Score struct {
	Skills     float64
	Experience float64
	Domain     float64
	Seniority  float64
	Location   float64
	Summary    float64
	Education  float64
	Title      float64
}

func (s *Score) Total() float64 {
	return s.Skills + s.Experience + s.Domain + s.Seniority + s.Location + s.Summary + s.Education + s.Title
}

type MatchInput struct {
	JobDescription string
	RelevantCV     string
	Scores         Score
}
