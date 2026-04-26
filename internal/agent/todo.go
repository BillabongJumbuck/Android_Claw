package agent

import (
	"errors"
	"fmt"
	"strings"
)

type TodoTask struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"` // pending, inProgress, completed
}

type TodoManager struct {
	Tasks []TodoTask
}

func NewTodoManager() *TodoManager {
	return &TodoManager{Tasks: make([]TodoTask, 0)}
}

func (tm *TodoManager) Update(tasks []TodoTask) (string, error) {
	if len(tasks) > 20 {
		return "", errors.New("Max 20 todos allowed!")
	}

	var validated []TodoTask
	inProgressCount := 0

	for i, task := range tasks {
		text := strings.TrimSpace(task.Text)
		status := task.Status
		if status == "" {
			status = "pending"
		}
		id := task.ID
		if id == "" {
			id = fmt.Sprintf("%d", i+1)
		}

		if text == "" {
			return "", fmt.Errorf("Task %s: text required!", id)
		}
		if status != "pending" && status != "inProgress" && status != "completed" {
			return "", fmt.Errorf("Task %s: invalid status '%s'", id, status)
		}
		if status == "inProgress" {
			inProgressCount++
		}

		validated = append(validated, TodoTask{ID: id, Text: text, Status: status})
	}

	if inProgressCount > 1 {
		return "", errors.New("Only one task can be in progress at a time.")
	}

	tm.Tasks = validated
	return tm.Render(), nil
}

func (tm *TodoManager) Render() string {
	if len(tm.Tasks) == 0 {
		return "No todos"
	}

	markers := map[string]string{
		"pending":    "[ ]",
		"completed":  "[x]",
		"inProgress": "[>]",
	}

	var lines []string
	completed := 0
	for _, task := range tm.Tasks {
		lines = append(lines, fmt.Sprintf("%s #%s: %s", markers[task.Status], task.ID, task.Text))
		if task.Status == "completed" {
			completed++
		}
	}
	lines = append(lines, fmt.Sprintf("\n(%d/%d completed)", completed, len(tm.Tasks)))
	return strings.Join(lines, "\n")
}
