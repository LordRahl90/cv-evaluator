package entities

type JobPost struct {
	ID         string
	Provider   string
	Title      string
	Company    string
	Location   string
	PostedAt   string
	DetailsURL string
}

type JobDetails struct {
	ID       string
	Details  string
	Provider string
}
