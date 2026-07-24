package main

import (
	"context"
	"cv-evaluator/db"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/scraper/linkedin"
	"cv-evaluator/internal/services/jobs"

	"github.com/joho/godotenv"
	"github.com/oklog/ulid/v2"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	userID, err := ulid.ParseStrict("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		log.Fatal(err)
	}
	//searchLink := "http://127.0.0.1:5500/linkedin/linkedin.html?position=1&pageNum=0"
	searchLink := "https://www.linkedin.com/jobs/search?keywords=Senior%20Software%20Engineer&location=Copenhagen&geoId=&trk=public_jobs_jobs-search-bar_search-submit&position=1&pageNum=0"

	dbase, err := db.SetupDatabase()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	embeddingLLMService := ollama.New(client, os.Getenv("OLLAMA_BASE_URL"))

	jobService := jobs.New(dbase, embeddingLLMService)

	linkedinService := linkedin.New()
	jobListings, err := linkedinService.SearchJobs(ctx, searchLink, 1)
	if err != nil {
		log.Fatal(err)
	}

	for _, job := range jobListings {
		// check if job has been processed for the user
		exists, err := jobService.IsJobProcessed(ctx, userID, job.ID)
		if err != nil {
			log.Fatal(err)
		}
		if exists != nil {
			fmt.Printf("job already processed for user %s: %s\n", userID.String(), job.ID)
			continue
		}

		fmt.Printf("\n\nProcessing job: %s\n\n", job.DetailsURL)
		details, err := linkedinService.GetJobDetails(ctx, &job)
		if err != nil {
			log.Fatal(err)
		}

		job.Details = details

		if err := jobService.ProcessJob(ctx, userID, &job); err != nil {
			log.Fatal(err)
		}

		time.Sleep(1 * time.Second)
	}
}
