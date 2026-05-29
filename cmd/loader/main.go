package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cv-solution/internal/llm/ollama"
	"cv-solution/internal/migrator"
	"cv-solution/internal/scraper/linkedin"
	"cv-solution/internal/services/jobs"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	userID := 1
	//searchLink := "http://127.0.0.1:5500/linkedin/linkedin.html?position=1&pageNum=0"
	searchLink := "https://www.linkedin.com/jobs/search?keywords=Senior%20Software%20Engineer&location=Copenhagen&geoId=&trk=public_jobs_jobs-search-bar_search-submit&position=1&pageNum=0"

	db, err := setupDatabase()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	embeddingLLMService := ollama.New(client, os.Getenv("OLLAMA_BASE_URL"))

	jobService := jobs.New(db, embeddingLLMService)

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
			fmt.Printf("job already processed for user %d: %s\n", userID, job.ID)
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

func setupDatabase() (*gorm.DB, error) {
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbParams := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := gorm.Open(postgres.Open(dbParams), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := migrator.Migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}
