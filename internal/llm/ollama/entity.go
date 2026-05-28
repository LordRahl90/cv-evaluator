package ollama

type Request struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Format string `json:"format"`
	Stream bool   `json:"stream"`
}

type Response struct {
	Response string `json:"response"`
}
