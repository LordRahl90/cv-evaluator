package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"cv-solution/internal/entities"
	"cv-solution/internal/models"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

var (
	breakDown = map[string]float64{
		"summary":    0.1,
		"education":  0.15,
		"experience": 0.25,
		"skills":     0.4,
		"title":      0.1,
	}
)

type EmbeddingLLMService interface {
	CreateEmbedding(ctx context.Context, content string) ([]float32, error)
}

type JobMatchLLMService interface {
	JobMatch(ctx context.Context, matchInput *entities.MatchInput) (string, error)
}

type Service struct {
	db           *gorm.DB
	embeddingLLM EmbeddingLLMService
	jobMatchLLM  JobMatchLLMService
}

func New(db *gorm.DB, embeddingLLM EmbeddingLLMService, jobMatchLLM JobMatchLLMService) *Service {
	return &Service{
		db:           db,
		embeddingLLM: embeddingLLM,
		jobMatchLLM:  jobMatchLLM,
	}
}

func (s *Service) MatchByJobID(ctx context.Context, userID int, jobID string) (*Response, error) {
	slog.DebugContext(ctx, "matching user CV with job description", "user_id", userID, "job_id", jobID)

	var job *models.Job
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND job_id = ?", userID, jobID).
		First(&job).Error; err != nil {
		return nil, fmt.Errorf("failed to retrieve job: %w", err)
	}

	return s.Match(ctx, userID, job.Detail)
}

func (s *Service) Match(ctx context.Context, userID int, jobDescription string) (*Response, error) {
	slog.DebugContext(ctx, "matching user CV with job description", "user_id", userID)

	jobDescriptionEmbeddings, err := s.embeddingLLM.CreateEmbedding(ctx, jobDescription)
	if err != nil {
		return nil, err
	}

	queryEmbedding := pgvector.NewVector(jobDescriptionEmbeddings)

	var results []MatchResult
	err = s.db.WithContext(ctx).
		Model(&models.SectionEmbedding{}).
		Select(
			"section_heading, section, embedding <=> ? AS distance, 1 - (embedding <=> ?) AS score",
			queryEmbedding, queryEmbedding,
		).
		Where("user_id = ?", userID).
		Order("distance ASC").
		Limit(10).
		Find(&results).Error
	if err != nil {
		return nil, err
	}

	scores := computeScores(results)

	matchInput := &entities.MatchInput{
		JobDescription: jobDescription,
		RelevantCV:     s.buildCVContext(ctx, results),
		Scores:         scores,
	}

	jobMatch, err := s.jobMatchLLM.JobMatch(ctx, matchInput)
	if err != nil {
		return nil, err
	}

	var response *Response
	if json.NewDecoder(strings.NewReader(jobMatch)).Decode(&response) != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}

	return response, nil
}

func computeScores(req []MatchResult) entities.Score {
	var (
		content = make(map[string]float64)
		result  = entities.Score{}
	)

	for _, v := range req {
		content[v.SectionHeading] = float64(v.Score)
	}

	skills := content["skills"]
	summary := content["summary"]
	experience := content["experience"]
	education := content["education"]
	title := content["title"]

	result.Skills = skills * breakDown["skills"]
	result.Experience = experience * breakDown["experience"]
	result.Domain = summary * breakDown["summary"]
	result.Education = education * breakDown["education"]
	result.Title = title * breakDown["title"]

	return result
}

func (s *Service) buildCVContext(ctx context.Context, input []MatchResult) string {
	slog.DebugContext(ctx, "building CV context", "input", input)
	grouped := groupSections(input)

	sortByScore(grouped.Skills)
	sortByScore(grouped.Experience)
	sortByScore(grouped.Summary)
	sortByScore(grouped.Other)
	var b strings.Builder
	// SKILLS (top 5)
	b.WriteString("[SKILLS - MOST RELEVANT]\n")
	for i, m := range grouped.Skills {
		if i >= 5 {
			break
		}
		b.WriteString(fmt.Sprintf("- %s\n", m.Content))
	}
	// EXPERIENCE (top 5)
	b.WriteString("\n[EXPERIENCE - MOST RELEVANT]\n")
	for i, m := range grouped.Experience {
		if i >= 5 {
			break
		}
		b.WriteString(fmt.Sprintf("- %s\n", m.Content))
	}
	// SUMMARY (top 2)
	b.WriteString("\n[SUMMARY]\n")
	for i, m := range grouped.Summary {
		if i >= 2 {
			break
		}
		b.WriteString(fmt.Sprintf("- %s\n", m.Content))
	}
	// OPTIONAL: OTHER CONTEXT (small cap)
	if len(grouped.Other) > 0 {
		b.WriteString("\n[ADDITIONAL CONTEXT]\n")
		for i, m := range grouped.Other {
			if i >= 3 {
				break
			}
			b.WriteString(fmt.Sprintf("- %s\n", m.Content))
		}
	}
	return b.String()
}

func groupSections(matches []MatchResult) CVContext {
	ctx := CVContext{}
	for _, m := range matches {
		switch strings.ToLower(m.SectionHeading) {
		case "skills":
			ctx.Skills = append(ctx.Skills, m)
		case "experience":
			ctx.Experience = append(ctx.Experience, m)
		case "summary":
			ctx.Summary = append(ctx.Summary, m)
		default:
			ctx.Other = append(ctx.Other, m)
		}
	}
	return ctx
}

func sortByScore(matches []MatchResult) {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}
