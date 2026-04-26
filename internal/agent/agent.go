package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type Agent struct {
	Client        *openai.Client
	ModelID       string
	WorkDir       string
	SkillsDir     string
	TranscriptDir string
	Threshold     int
	KeepRecent    int
	SkillLoader   *SkillLoader
	Todo          *TodoManager
}

func NewAgent(client *openai.Client, modelID, workDir string) (*Agent, error) {
	skillsDir := filepath.Join(workDir, "skills")
	loader, err := NewSkillLoader(skillsDir)
	if err != nil {
		return nil, err
	}

	return &Agent{
		Client:        client,
		ModelID:       modelID,
		WorkDir:       workDir,
		SkillsDir:     skillsDir,
		TranscriptDir: filepath.Join(workDir, ".transcripts"),
		Threshold:     50000,
		KeepRecent:    3,
		SkillLoader:   loader,
		Todo:          NewTodoManager(),
	}, nil
}

func (a *Agent) GetSystemPrompt() string {
	return fmt.Sprintf(`You are AndroidClaw, an AI for OS Native Agent running directly in the Android shell.
Your working directory is %s (typically /data/local/tmp).

You are an expert in Android system internals, CLI tools (like adb shell commands, pm, am, input, uiautomator), and UI automation. The user is relying on you to navigate the OS and execute tasks autonomously.
When interacting with the UI, you should observe the screen state, understand the XML hierarchy, calculate precise center coordinates from bounds, and execute actions.

Rules for execution:
1. Use the todo tool to break down complex OS tasks into verifiable steps. Mark 'inProgress' before starting, 'completed' when done.
2. A maximum of 20 Todos are allowed. Only one can be inProgress at a time.
3. Verify your actions. If you click a button, check if the UI changed as expected before proceeding.
4. Prefer executing tools over explaining in prose.
5. Use loadSkill to access specialized knowledge before tackling unfamiliar Android workflows.

Skills available:
%s`, a.WorkDir, a.SkillLoader.GetDescriptions())
}

// 统一的工具 Schema 注册表
func (a *Agent) getChildTools() []openai.Tool {
	return []openai.Tool{
		{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "bash", Description: "Run a shell command.", Parameters: jsonschema.Definition{Type: jsonschema.Object, Properties: map[string]jsonschema.Definition{"command": {Type: jsonschema.String}}, Required: []string{"command"}}}},
		{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "readFile", Description: "Read file contents.", Parameters: jsonschema.Definition{Type: jsonschema.Object, Properties: map[string]jsonschema.Definition{"filePath": {Type: jsonschema.String}, "limit": {Type: jsonschema.Integer}}, Required: []string{"filePath"}}}},
		{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "writeFile", Description: "Write content to file.", Parameters: jsonschema.Definition{Type: jsonschema.Object, Properties: map[string]jsonschema.Definition{"filePath": {Type: jsonschema.String}, "content": {Type: jsonschema.String}}, Required: []string{"filePath", "content"}}}},
		{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "todo", Description: "Update task list.", Parameters: jsonschema.Definition{Type: jsonschema.Object, Properties: map[string]jsonschema.Definition{
			"tasks": {Type: jsonschema.Array, Items: &jsonschema.Definition{Type: jsonschema.Object, Properties: map[string]jsonschema.Definition{"id": {Type: jsonschema.String}, "text": {Type: jsonschema.String}, "status": {Type: jsonschema.String}}, Required: []string{"id", "text", "status"}}},
		}, Required: []string{"tasks"}}}},
		{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "loadSkill", Description: "Load knowledge by name.", Parameters: jsonschema.Definition{Type: jsonschema.Object, Properties: map[string]jsonschema.Definition{"name": {Type: jsonschema.String}}, Required: []string{"name"}}}},
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "getUI",
				Description: "Get the current Android screen UI hierarchy as a simplified JSON list of elements with their center coordinates (cx, cy). Use this to 'see' the screen before deciding where to tap or swipe.",
			},
		},
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "inputText",
				Description: "Type text into the currently focused input field. Use this AFTER tapping on a text box. It supports Chinese, English, spaces, and special characters. It automatically presses the ENTER/Search key after typing.",
				Parameters: jsonschema.Definition{
					Type: jsonschema.Object,
					Properties: map[string]jsonschema.Definition{
						"text": {Type: jsonschema.String, Description: "The text you want to type"},
					},
					Required: []string{"text"},
				},
			},
		},
	}
}

func (a *Agent) executeTool(name, arguments string) (string, bool) {
	var args map[string]interface{}
	json.Unmarshal([]byte(arguments), &args)

	getString := func(k string) string { v, _ := args[k].(string); return v }
	usedTodo := false
	var result string

	switch name {
	case "bash":
		result = a.RunBash(getString("command"))
	case "readFile":
		limit := 0
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		result = a.RunRead(getString("filePath"), limit)
	case "writeFile":
		result = a.RunWrite(getString("filePath"), getString("content"))
	case "todo":
		usedTodo = true
		var tArgs struct {
			Tasks []TodoTask `json:"tasks"`
		}
		json.Unmarshal([]byte(arguments), &tArgs)
		res, err := a.Todo.Update(tArgs.Tasks)
		if err != nil {
			result = err.Error()
		} else {
			result = res
		}
	case "loadSkill":
		result = a.SkillLoader.GetContent(getString("name"))
	case "getUI":
		result = a.GetUIHierarchy()
	case "inputText":
		result = a.RunInputText(getString("text"))
	default:
		result = "Unknown tool: " + name
	}
	return result, usedTodo
}

func (a *Agent) Loop(messages *[]openai.ChatCompletionMessage) error {
	for {
		*messages = a.MicroCompact(*messages)

		if EstimateTokens(*messages) > a.Threshold {
			fmt.Println("\033[35m[auto_compact triggered]\033[0m")
			compressed, _ := a.AutoCompact(*messages)
			*messages = compressed
		}

		resp, err := a.Client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
			Model: a.ModelID, Messages: *messages, Tools: a.getChildTools(),
		})
		if err != nil {
			return err
		}

		msg := resp.Choices[0].Message
		*messages = append(*messages, msg)

		if len(msg.ToolCalls) == 0 {
			return nil
		}

		var toolMessages []openai.ChatCompletionMessage
		for _, call := range msg.ToolCalls {
			result, _ := a.executeTool(call.Function.Name, call.Function.Arguments)
			fmt.Printf("\033[33m> %s: %s...\033[0m\n", call.Function.Name, truncate(result, 100))
			toolMessages = append(toolMessages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleTool, Content: result, ToolCallID: call.ID})
		}

		*messages = append(*messages, toolMessages...)
	}
}
