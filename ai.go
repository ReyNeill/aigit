package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "os"
    "strings"
    "time"
)

type chatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type chatRequest struct {
    Model       string        `json:"model"`
    Messages    []chatMessage `json:"messages"`
    Temperature float64       `json:"temperature,omitempty"`
    MaxTokens   int           `json:"max_tokens,omitempty"`
}

type chatResponse struct {
    Choices []struct {
        Message struct {
            Content string `json:"content"`
        } `json:"message"`
    } `json:"choices"`
}

func summarizeWithAI(model string) (string, error) {
    diff, err := git("diff", "--name-status", "-M", "-C")
    if err != nil {
        return "", err
    }
    diff = strings.TrimSpace(diff)
    if diff == "" {
        return "", nil
    }
    // Include conflicts if any
    var conflictNote string
    if isMerging() {
        if conflicts, _ := listConflicts(); len(conflicts) > 0 {
            conflictNote = "\nConflicts: " + strings.Join(conflicts, ", ")
        }
    }

    prompt := "Generate a concise one-line summary (<=15 words) of current code changes suitable as a commit subject. Use imperative mood, mention key files or intent. Do not include punctuation at end.\n\nChanged files (git name-status):\n" + diff + conflictNote

    key := os.Getenv("OPENROUTER_API_KEY")
    if key == "" {
        return "", errors.New("OPENROUTER_API_KEY not set")
    }

    req := chatRequest{
        Model:       model,
        Temperature: 0.2,
        Messages: []chatMessage{
            {Role: "system", Content: "You write crisp one-line git commit subjects."},
            {Role: "user", Content: prompt},
        },
        MaxTokens: 50,
    }
    body, _ := json.Marshal(req)

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Authorization", "Bearer "+key)
    // OpenRouter encourages one of these headers for attribution; safe defaults.
    httpReq.Header.Set("X-Title", "Aigit")

    resp, err := http.DefaultClient.Do(httpReq)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return "", fmt.Errorf("openrouter status %d", resp.StatusCode)
    }
    var cr chatResponse
    if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
        return "", err
    }
    if len(cr.Choices) == 0 {
        return "", errors.New("no choices from OpenAI")
    }
    s := strings.TrimSpace(cr.Choices[0].Message.Content)
    // Keep it to single line
    s = strings.ReplaceAll(s, "\n", " ")
    s = strings.TrimSpace(s)
    return s, nil
}
