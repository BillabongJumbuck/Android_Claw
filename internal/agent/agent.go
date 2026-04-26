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
2. Verify your actions. If you click a button, check if the UI changed as expected before proceeding.
3. To type text, ALWAYS use the 'inputText' tool. Do NOT use bash for 'input text' commands.
4. CRITICAL: 'getUI', 'inputText', and 'todo' are Agent Tools, NOT shell commands. NEVER pass them into the 'bash' tool.
5. CRITICAL STOPPING RULE: Once you have successfully verified that the user's goal is accomplished, YOU MUST STOP CALLING TOOLS. Output a final, friendly plain-text response to the user summarizing what you did to complete the turn.

Skills available:
%s`, a.WorkDir, a.SkillLoader.GetDescriptions())
}

// 统一的工具 Schema 注册表
func (a *Agent) getTools() []openai.Tool {
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
				Description: "Type text into the currently focused input field. Use this AFTER tapping on a text box. It only supports ascii characters. It automatically presses the ENTER/Search key after typing.",
				Parameters: jsonschema.Definition{
					Type: jsonschema.Object,
					Properties: map[string]jsonschema.Definition{
						"text": {Type: jsonschema.String, Description: "The text you want to type"},
					},
					Required: []string{"text"},
				},
			},
		},
		// 1. 注册 swipe (滑动与长按)
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "swipe",
				Description: "Swipe on the screen from (x1, y1) to (x2, y2) over a specific duration. To scroll DOWN to see more content, swipe from bottom to top (e.g., y1=1500 to y2=500). To LONG PRESS, set x1=x2 and y1=y2, with duration=1000.",
				Parameters: jsonschema.Definition{
					Type: jsonschema.Object,
					Properties: map[string]jsonschema.Definition{
						"x1":       {Type: jsonschema.Integer, Description: "Start X coordinate"},
						"y1":       {Type: jsonschema.Integer, Description: "Start Y coordinate"},
						"x2":       {Type: jsonschema.Integer, Description: "End X coordinate"},
						"y2":       {Type: jsonschema.Integer, Description: "End Y coordinate"},
						"duration": {Type: jsonschema.Integer, Description: "Duration in milliseconds (e.g., 300 for quick swipe, 1000 for slow swipe or long press)"},
					},
					Required: []string{"x1", "y1", "x2", "y2", "duration"},
				},
			},
		},
		// 2. 注册 keyevent (系统按键)
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "keyevent",
				Description: "Send an Android system key event. Common codes: 3 (HOME), 4 (BACK), 66 (ENTER), 26 (POWER), 24 (VOL UP), 25 (VOL DOWN).",
				Parameters: jsonschema.Definition{
					Type: jsonschema.Object,
					Properties: map[string]jsonschema.Definition{
						"keycode": {Type: jsonschema.Integer, Description: "The keycode number"},
					},
					Required: []string{"keycode"},
				},
			},
		},
	}
}

func (a *Agent) executeTool(name, arguments string) (string, bool) {
	var args map[string]interface{}
	json.Unmarshal([]byte(arguments), &args)

	getString := func(k string) string { v, _ := args[k].(string); return v }
	getInt := func(k string) int {
		if v, ok := args[k].(float64); ok {
			return int(v)
		}
		return 0
	}

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
	case "swipe":
		result = a.RunSwipe(getInt("x1"), getInt("y1"), getInt("x2"), getInt("y2"), getInt("duration"))
	case "keyevent":
		result = a.RunKeyevent(getInt("keycode"))
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
			Model: a.ModelID, Messages: *messages, Tools: a.getTools(),
		})
		if err != nil {
			return err
		}

		msg := resp.Choices[0].Message

		// if msg.ReasoningContent != "" {
		// 	msg.Content = "<think>\n" + msg.ReasoningContent + "\n</think>\n" + msg.Content
		// 	msg.ReasoningContent = "" // 清空，防止 SDK 序列化异常
		// }

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
