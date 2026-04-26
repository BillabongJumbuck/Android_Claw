package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/BillabongJumbuck/Android_Claw/internal/agent"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

func main() {
	godotenv.Load() // 加载 .env

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("Error: DEEPSEEK_API_KEY is not set.")
		return
	}

	baseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	modelID := os.Getenv("MODEL_ID")
	if modelID == "" {
		modelID = "deepseek-chat" // 默认模型
	}

	workDir, _ := os.Getwd()

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 10 * time.Second}
				// 强制使用 Google 的 8.8.8.8 或国内的 114.114.114.114 进行 DNS 解析
				return d.DialContext(ctx, "udp", "8.8.8.8:53")
			},
		},
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL
	config.HTTPClient = httpClient
	client := openai.NewClientWithConfig(config)

	coreAgent, err := agent.NewAgent(client, modelID, workDir)
	if err != nil {
		fmt.Printf("Failed to initialize agent: %v\n", err)
		return
	}

	history := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: coreAgent.GetSystemPrompt()},
	}

	fmt.Println("\033[32mAgent Core switched to DeepSeek/OpenAI Go Engine. Ready.\033[0m")
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("\033[36ms01 >> \033[0m")
		if !scanner.Scan() {
			break
		}
		query := scanner.Text()
		if query == "q" || query == "exit" {
			break
		}
		if strings.TrimSpace(query) == "" {
			continue
		}

		history = append(history, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: query})

		err := coreAgent.Loop(&history)
		if err != nil {
			fmt.Printf("Error in agent loop: %v\n", err)
			break
		}

		lastMsg := history[len(history)-1]
		if lastMsg.Role == openai.ChatMessageRoleAssistant && lastMsg.Content != "" {
			fmt.Println(lastMsg.Content)
		}
		fmt.Println()
	}
}
