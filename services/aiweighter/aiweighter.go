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

	/**
		 * The response from OpenRouter is a JSON object that contains the model's response.

		 {"id":"gen-x-x","object":"chat.completion","created":1791895087,"model":"nvidia/nemotron-3-ultra-550b-a55b:free","provider":"Nvidia","system_fingerprint":null,"
	  │   service_tier":null,"choices":[{"index":0,"logprobs":null,"finish_reason":"stop","native_finish_reason":"stop","message":{"role":"assistant","content":"2 + 2 = 4","refusal":null,"reasoning"
	  │   :"The user is asking a very simple math question: 2 + 2.\nThe answer is 4.","reasoning_details":[{"type":"reasoning.text","text":"The user is asking a very simple math question: 2 + 2.\nTh
	  │   e answer is 4.","format":"unknown","index":0}]}}],"usage":{"prompt_tokens":23,"completion_tokens":31,"total_tokens":54,"cost":0,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"
	  │   cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0,"upstream_inference_prompt_cost":0,"upstream_inference_completions_cost":0},"completion
	  │   _tokens_details":{"reasoning_tokens":19,"image_tokens":0,"audio_tokens":0}}}

		So basically, we need to parse the JSON and extract the content of the first choice's message.
	*/
	finalResponse := struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
	}{}

	if err := json.Unmarshal(responseBody, &finalResponse); err != nil {
		panic(err)
	}

	if len(finalResponse.Choices) == 0 {
		panic("No choices returned from OpenRouter")
	}

	fmt.Println("[THIS] OpenRouter response:", finalResponse.Choices[0].Message.Content)
	// optionally, you can also print the reasoning if you want
	fmt.Println("[THIS] OpenRouter reasoning:", finalResponse.Choices[0].Message.Reasoning)

	fmt.Println(string(responseBody))
}
