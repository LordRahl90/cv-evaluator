package entities

import "cv-evaluator/internal/models"

type JobPost struct {
	ID         string
	Provider   string
	Title      string
	Company    string
	Location   string
	PostedAt   string
	DetailsURL string
	Details    *JobDetails
}

func (j *JobPost) ToModel() *models.Job {
	res := &models.Job{
		JobID:      j.ID,
		Provider:   j.Provider,
		Title:      j.Title,
		Company:    j.Company,
		Location:   j.Location,
		PostedAt:   j.PostedAt,
		DetailsURL: j.DetailsURL,
		Status:     models.JobStatusNew,
	}

	if j.Details != nil && j.Details.Details != "" {
		res.Detail = j.Details.Details
		res.Status = models.JobStatusDescribed
		res.Visited = true
	}

	return res
}

type JobDetails struct {
	ID       string
	Details  string
	Provider string
	Location string
	PostedAt string
}
