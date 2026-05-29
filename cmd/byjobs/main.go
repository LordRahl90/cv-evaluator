package main

import (
	"context"
	"cv-solution/internal/services/matcher"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cv-solution/internal/llm/deepseek"
	"cv-solution/internal/llm/ollama"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	userID int
	jobID  string
)

func main() {
	flag.IntVar(&userID, "user-id", 1, "User ID")
	flag.StringVar(&jobID, "job-id", "", "Job ID")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	db, err := setupDatabase()
	if err != nil {
		log.Fatal(err)
	}

	// llm service
	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	chatLLM := deepseek.New(deepseekKey)
	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	embeddingLLMService := ollama.New(client, os.Getenv("OLLAMA_BASE_URL"))

	matchService := matcher.New(db, embeddingLLMService, chatLLM)

	res, err := matchService.MatchByJobID(context.Background(), userID, jobID)
	if err != nil {
		log.Fatal(err)
	}

	b, err := json.Marshal(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n\n%s\n\n", b)
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

	//if err := migrator.Migrate(db); err != nil {
	//	return nil, err
	//}

	return db, nil
}
