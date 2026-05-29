package main

import (
	"context"
	"cv-solution/internal/llm/deepseek"
	"cv-solution/internal/llm/ollama"
	"cv-solution/internal/services/matcher"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
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

	// we process cv

	//b, err := os.ReadFile("./data/description.txt")
	b, err := os.ReadFile("./data/another-desc.txt")
	if err != nil {
		log.Fatal(err)
	}

	matchingService := matcher.New(db, embeddingLLMService, chatLLM)
	res, err := matchingService.Match(context.TODO(), 1, string(b))
	if err != nil {
		log.Fatal(err)
	}

	bout, err := json.Marshal(res)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n\n%s\n\n", bout)
}

func createUser() {
	//userService := users.New(db, chatLLM, embeddingLLMService)

	// first we create the demo user
	//user, err := userService.Create(context.TODO(), users.CreateUserRequest{
	//	FirstName: "Abiodun",
	//	LastName:  "Alugbin",
	//	Email:     "alugbin.abiodun@gmail.com",
	//	Phone:     "+4571358113",
	//	Password:  "password123",
	//})
	//if err != nil {
	//	log.Fatal(err)
	//}
	//
	//fmt.Printf("\n\nUser is: %+v\n\n", user)
}

func processCV() {
	//cvFile, err := os.Open("./data/alugbin-abiodun-resume.pdf")
	//if err != nil {
	//	log.Fatal(err)
	//}
	//defer func() {
	//	_ = cvFile.Close()
	//}()
	//
	//if err := userService.ProcessCV(context.TODO(), 1, cvFile); err != nil {
	//	log.Fatal(err)
	//}
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
