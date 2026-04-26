package agent

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type XMLNode struct {
	Text        string    `xml:"text,attr"`
	ContentDesc string    `xml:"content-desc,attr"`
	Clickable   string    `xml:"clickable,attr"`
	Bounds      string    `xml:"bounds,attr"`
	Nodes       []XMLNode `xml:"node"`
}

type UIElement struct {
	ID   int    `json:"id"`
	Text string `json:"text,omitempty"`
	Desc string `json:"desc,omitempty"`
	CX   int    `json:"cx"` // 中心点 X 坐标
	CY   int    `json:"cy"` // 中心点 Y 坐标
}

func (a *Agent) getSafePath(p string) (string, error) {
	resolvedPath, err := filepath.Abs(filepath.Join(a.WorkDir, p))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(resolvedPath, a.WorkDir) {
		return "", fmt.Errorf("Path escapes working directory: %s", p)
	}
	return resolvedPath, nil
}

func (a *Agent) RunBash(command string) string {
	dangerous := []string{"rm -rf /", "sudo", "shutdown", "reboot", "> /dev"}
	for _, d := range dangerous {
		if strings.Contains(command, d) {
			return "Error: Dangerous command blocked"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = a.WorkDir

	outBytes, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(outBytes))

	if ctx.Err() == context.DeadlineExceeded {
		return "Error: Timeout (120s)\n" + truncate(out, 50000)
	}

	if err != nil && out == "" {
		return fmt.Sprintf("Error: %v", err)
	}

	if out == "" {
		return "(no output)"
	}
	return truncate(out, 50000)
}

func (a *Agent) RunRead(filePath string, limit int) string {
	fullPath, err := a.getSafePath(filePath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	if limit > 0 && len(lines) > limit {
		lines = append(lines[:limit], fmt.Sprintf("... (%d more lines)", len(lines)-limit))
	}

	return truncate(strings.Join(lines, "\n"), 50000)
}

func (a *Agent) RunWrite(filePath, content string) string {
	fullPath, err := a.getSafePath(filePath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	os.MkdirAll(filepath.Dir(fullPath), 0755)
	err = os.WriteFile(fullPath, []byte(content), 0644)
	if err != nil {
		return fmt.Sprintf("Error writing file: %v", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), filePath)
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// 递归遍历 XML 树，提取有效节点
func extractUIElements(node XMLNode, elements *[]UIElement, counter *int) {
	// 如果节点有实际内容，或者是可点击的，我们就保留它
	if node.Text != "" || node.ContentDesc != "" || node.Clickable == "true" {
		// 解析 bounds="[x1,y1][x2,y2]"
		re := regexp.MustCompile(`\[(\d+),(\d+)\]\[(\d+),(\d+)\]`)
		matches := re.FindStringSubmatch(node.Bounds)
		if len(matches) == 5 {
			x1, _ := strconv.Atoi(matches[1])
			y1, _ := strconv.Atoi(matches[2])
			x2, _ := strconv.Atoi(matches[3])
			y2, _ := strconv.Atoi(matches[4])

			// 计算中心点，LLM 只需要知道点哪里就行
			cx := (x1 + x2) / 2
			cy := (y1 + y2) / 2

			*elements = append(*elements, UIElement{
				ID:   *counter,
				Text: node.Text,
				Desc: node.ContentDesc,
				CX:   cx,
				CY:   cy,
			})
			*counter++
		}
	}

	for _, child := range node.Nodes {
		extractUIElements(child, elements, counter)
	}
}

// 获取并解析 UI 的 Tool 实现
func (a *Agent) GetUIHierarchy() string {
	dumpPath := filepath.Join(a.WorkDir, "window_dump.xml")

	// 1. 调用安卓底层导出界面树
	a.RunBash(fmt.Sprintf("uiautomator dump %s", dumpPath))

	// 2. 读取导出的 XML 文件
	content, err := os.ReadFile(dumpPath)
	if err != nil {
		return fmt.Sprintf("Error reading UI dump: %v", err)
	}

	// 3. 解析 XML
	var root XMLNode
	err = xml.Unmarshal(content, &root)
	if err != nil {
		return fmt.Sprintf("Error parsing XML: %v", err)
	}

	// 4. 提炼有用信息
	var elements []UIElement
	counter := 1
	extractUIElements(root, &elements, &counter)

	// 5. 将精简后的 JSON 返回给 LLM
	result, _ := json.Marshal(elements)
	return string(result)
}

func (a *Agent) RunInputText(text string) string {
	// 1. 判断是否为纯 ASCII (纯英文/数字)
	isAscii := true
	for _, c := range text {
		if c > 127 {
			isAscii = false
			break
		}
	}

	if isAscii {
		// ==========================================
		// 策略 A：纯英文字符，使用 100% 可靠的原生 input text
		// ==========================================
		// 安卓 input text 的致命缺陷是不能有真实空格，必须替换为 %s
		safeText := strings.ReplaceAll(text, " ", "%s")

		// 过滤掉可能引起 shell 截断的特殊字符
		safeText = strings.ReplaceAll(safeText, "'", "\\'")
		safeText = strings.ReplaceAll(safeText, "\"", "\\\"")
		safeText = strings.ReplaceAll(safeText, "&", "\\&")

		// 直接模拟按键输入
		a.RunBash(fmt.Sprintf("input text '%s'", safeText))

		time.Sleep(300 * time.Millisecond)
		a.RunBash("input keyevent 66") // KEYCODE_ENTER 触发搜索

		return fmt.Sprintf("Successfully inputted ASCII text: %s", text)

	} else {
		// ==========================================
		// 策略 B：包含中文，尝试绕过系统的剪贴板黑科技
		// ==========================================
		safeText := strings.ReplaceAll(text, "'", "")

		// 尝试写入剪贴板 (不同厂商的 ROM 支持的命令不同，我们全量尝试)
		a.RunBash(fmt.Sprintf("cmd clipboard set '%s'", safeText))

		// 给予系统足够的响应时间
		time.Sleep(500 * time.Millisecond)

		// 发送全局粘贴指令 KEYCODE_PASTE
		a.RunBash("input keyevent 279")
		time.Sleep(300 * time.Millisecond)

		// 触发回车搜索
		a.RunBash("input keyevent 66")

		return fmt.Sprintf("Attempted to paste Unicode text: %s", text)
	}
}
