package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sashabaranov/go-openai"
)

func EstimateTokens(messages []openai.ChatCompletionMessage) int {
	b, _ := json.Marshal(messages)
	return len(b) / 4
}

func (a *Agent) MicroCompact(messages []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	var toolResultIndices []int
	toolNameMap := make(map[string]string)

	for i, msg := range messages {
		if msg.Role == openai.ChatMessageRoleAssistant && len(msg.ToolCalls) > 0 {
			for _, call := range msg.ToolCalls {
				toolNameMap[call.ID] = call.Function.Name
			}
		}
		if msg.Role == openai.ChatMessageRoleTool {
			toolResultIndices = append(toolResultIndices, i)
		}
	}

	if len(toolResultIndices) <= a.KeepRecent {
		return messages
	}

	toClearCount := len(toolResultIndices) - a.KeepRecent
	for _, idx := range toolResultIndices[:toClearCount] {
		if len(messages[idx].Content) > 100 {
			toolName := toolNameMap[messages[idx].ToolCallID]
			if toolName == "" {
				toolName = "unknown_tool"
			}
			messages[idx].Content = fmt.Sprintf("[Previous: used %s]", toolName)
		}
	}
	return messages
}

func (a *Agent) AutoCompact(messages []openai.ChatCompletionMessage) ([]openai.ChatCompletionMessage, error) {
	os.MkdirAll(a.TranscriptDir, 0755)
	timestamp := time.Now().Unix()
	transcriptPath := filepath.Join(a.TranscriptDir, fmt.Sprintf("transcript_%d.jsonl", timestamp))

	var jsonlData string
	for _, msg := range messages {
		b, _ := json.Marshal(msg)
		jsonlData += string(b) + "\n"
	}
	os.WriteFile(transcriptPath, []byte(jsonlData), 0644)
	fmt.Printf("\033[36m[transcript saved: %s]\033[0m\n", transcriptPath)

	conversationText := truncate(jsonlData, 80000)

	summaryReq := openai.ChatCompletionRequest{
		Model: a.ModelID,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: "Summarize this conversation for continuity. Include: 1) What was accomplished, 2) Current state, 3) Key decisions made. Be concise but preserve critical details.\n\n" + conversationText,
			},
		},
		MaxTokens: 2000,
	}

	resp, err := a.Client.CreateChatCompletion(context.Background(), summaryReq)
	var summary string
	if err != nil {
		summary = "(No summary generated due to error: " + err.Error() + ")"
	} else {
		summary = resp.Choices[0].Message.Content
	}

	return []openai.ChatCompletionMessage{
		messages[0], // Keep System Prompt
		{Role: openai.ChatMessageRoleUser, Content: fmt.Sprintf("[Conversation compressed. Transcript: %s]\n\n%s", transcriptPath, summary)},
		{Role: openai.ChatMessageRoleAssistant, Content: "Understood. I have the context from the summary. Continuing."},
	}, nil
}
