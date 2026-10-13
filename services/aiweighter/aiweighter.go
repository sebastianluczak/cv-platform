package aiweighter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const endpoint = "https://openrouter.ai/api/v1/chat/completions"

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func SendToWeighter(content string) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		panic("OPENROUTER_API_KEY is not set")
	}

	payload := ChatRequest{
		Model: "nvidia/nemotron-3-ultra-550b-a55b:free",
		Messages: []Message{
			{
				Role:    "user",
				Content: content,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		panic(fmt.Sprintf(
			"OpenRouter returned HTTP %d: %s",
			resp.StatusCode,
			responseBody,
		))
	}

	fmt.Println(string(responseBody))
}
