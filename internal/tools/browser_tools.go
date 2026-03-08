package tools

import (
	"context"
	"encoding/json"
	"time"
)

// Browser tool type definitions

type BrowserGotoTool struct{ mgr *BrowserManager }
type BrowserGoBackTool struct{ mgr *BrowserManager }
type BrowserGetPageStateTool struct{ mgr *BrowserManager }
type BrowserTakeScreenshotTool struct{ mgr *BrowserManager }
type BrowserClickTool struct{ mgr *BrowserManager }
type BrowserFillTextTool struct{ mgr *BrowserManager }
type BrowserPressKeyTool struct{ mgr *BrowserManager }
type BrowserScrollTool struct{ mgr *BrowserManager }

func NewBrowserGotoTool(mgr *BrowserManager) *BrowserGotoTool { return &BrowserGotoTool{mgr: mgr} }
func NewBrowserGoBackTool(mgr *BrowserManager) *BrowserGoBackTool {
	return &BrowserGoBackTool{mgr: mgr}
}
func NewBrowserGetPageStateTool(mgr *BrowserManager) *BrowserGetPageStateTool {
	return &BrowserGetPageStateTool{mgr: mgr}
}
func NewBrowserTakeScreenshotTool(mgr *BrowserManager) *BrowserTakeScreenshotTool {
	return &BrowserTakeScreenshotTool{mgr: mgr}
}
func NewBrowserClickTool(mgr *BrowserManager) *BrowserClickTool { return &BrowserClickTool{mgr: mgr} }
func NewBrowserFillTextTool(mgr *BrowserManager) *BrowserFillTextTool {
	return &BrowserFillTextTool{mgr: mgr}
}
func NewBrowserPressKeyTool(mgr *BrowserManager) *BrowserPressKeyTool {
	return &BrowserPressKeyTool{mgr: mgr}
}
func NewBrowserScrollTool(mgr *BrowserManager) *BrowserScrollTool {
	return &BrowserScrollTool{mgr: mgr}
}

// BrowserGotoTool

type gotoArgs struct {
	URL       string `json:"url"`
	WaitMS    int    `json:"wait_ms"`
	TimeoutMS int    `json:"timeout_ms"`
}

func (t *BrowserGotoTool) Name() string        { return "browser_goto" }
func (t *BrowserGotoTool) Description() string { return "Navigate to a URL in the browser." }
func (t *BrowserGotoTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Target URL (http/https).",
			},
			"wait_ms": map[string]any{
				"type":        "integer",
				"description": "Optional wait time after navigation (milliseconds).",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Navigation timeout in milliseconds.",
			},
		},
		"required": []string{"url"},
	}
}
func (t *BrowserGotoTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var payload gotoArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	timeout := defaultBrowserTimeout
	if payload.TimeoutMS > 0 {
		timeout = time.Duration(payload.TimeoutMS) * time.Millisecond
	}
	state, err := t.mgr.Goto(ctx, tc.SessionID, payload.URL, payload.WaitMS, timeout)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// BrowserGoBackTool

type goBackArgs struct {
	WaitMS    int `json:"wait_ms"`
	TimeoutMS int `json:"timeout_ms"`
}

func (t *BrowserGoBackTool) Name() string        { return "browser_go_back" }
func (t *BrowserGoBackTool) Description() string { return "Go back to the previous page." }
func (t *BrowserGoBackTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"wait_ms": map[string]any{
				"type":        "integer",
				"description": "Optional wait time after navigation (milliseconds).",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Navigation timeout in milliseconds.",
			},
		},
	}
}
func (t *BrowserGoBackTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var payload goBackArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
	}
	timeout := defaultBrowserTimeout
	if payload.TimeoutMS > 0 {
		timeout = time.Duration(payload.TimeoutMS) * time.Millisecond
	}
	state, err := t.mgr.GoBack(ctx, tc.SessionID, payload.WaitMS, timeout)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// BrowserGetPageStateTool

type getPageStateArgs struct {
	MaxChars int `json:"max_chars"`
}

func (t *BrowserGetPageStateTool) Name() string { return "browser_get_page_state" }
func (t *BrowserGetPageStateTool) Description() string {
	return "Extract a simplified page state with interactive element IDs."
}
func (t *BrowserGetPageStateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"max_chars": map[string]any{
				"type":        "integer",
				"description": "Maximum number of characters of page text to return.",
			},
		},
	}
}
func (t *BrowserGetPageStateTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	_ = ctx
	var payload getPageStateArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
	}
	state, err := t.mgr.GetPageState(tc.SessionID, payload.MaxChars)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// BrowserTakeScreenshotTool

type takeScreenshotArgs struct {
	FullPage bool `json:"full_page"`
}

type takeScreenshotResult struct {
	ContentType string    `json:"content_type"`
	Data        string    `json:"data"`
	State       pageState `json:"state"`
}

func (t *BrowserTakeScreenshotTool) Name() string { return "browser_take_screenshot" }
func (t *BrowserTakeScreenshotTool) Description() string {
	return "Take a screenshot with interactive element IDs annotated."
}
func (t *BrowserTakeScreenshotTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"full_page": map[string]any{
				"type":        "boolean",
				"description": "Capture full page if true.",
			},
		},
	}
}
func (t *BrowserTakeScreenshotTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	_ = ctx
	var payload takeScreenshotArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
	}
	data, state, err := t.mgr.TakeScreenshot(tc.SessionID, payload.FullPage)
	if err != nil {
		return "", err
	}
	out := takeScreenshotResult{
		ContentType: "image/png",
		Data:        data,
		State:       state,
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// BrowserClickTool

type clickArgs struct {
	ElementID int `json:"element_id"`
}

func (t *BrowserClickTool) Name() string { return "browser_click" }
func (t *BrowserClickTool) Description() string {
	return "Click an element by element_id from get_page_state."
}
func (t *BrowserClickTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"element_id": map[string]any{
				"type":        "integer",
				"description": "Element ID returned by get_page_state.",
			},
		},
		"required": []string{"element_id"},
	}
}
func (t *BrowserClickTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	_ = ctx
	var payload clickArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if err := t.mgr.Click(tc.SessionID, payload.ElementID); err != nil {
		return "", err
	}
	return "ok", nil
}

// BrowserFillTextTool

type fillTextArgs struct {
	ElementID int    `json:"element_id"`
	Text      string `json:"text"`
}

func (t *BrowserFillTextTool) Name() string { return "browser_fill_text" }
func (t *BrowserFillTextTool) Description() string {
	return "Fill text into an input element by element_id."
}
func (t *BrowserFillTextTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"element_id": map[string]any{
				"type":        "integer",
				"description": "Element ID returned by get_page_state.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to input.",
			},
		},
		"required": []string{"element_id", "text"},
	}
}
func (t *BrowserFillTextTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	_ = ctx
	var payload fillTextArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if err := t.mgr.FillText(tc.SessionID, payload.ElementID, payload.Text); err != nil {
		return "", err
	}
	return "ok", nil
}

// BrowserPressKeyTool

type pressKeyArgs struct {
	Key string `json:"key"`
}

func (t *BrowserPressKeyTool) Name() string        { return "browser_press_key" }
func (t *BrowserPressKeyTool) Description() string { return "Press a keyboard key." }
func (t *BrowserPressKeyTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "Key to press (e.g., Enter, Tab).",
			},
		},
		"required": []string{"key"},
	}
}
func (t *BrowserPressKeyTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	_ = ctx
	var payload pressKeyArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if err := t.mgr.PressKey(tc.SessionID, payload.Key); err != nil {
		return "", err
	}
	return "ok", nil
}

// BrowserScrollTool

type scrollArgs struct {
	Direction string `json:"direction"`
	Amount    int    `json:"amount"`
}

func (t *BrowserScrollTool) Name() string { return "browser_scroll" }
func (t *BrowserScrollTool) Description() string {
	return "Scroll the page up or down by a pixel amount."
}
func (t *BrowserScrollTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"direction": map[string]any{
				"type":        "string",
				"description": "Scroll direction: up or down.",
			},
			"amount": map[string]any{
				"type":        "integer",
				"description": "Scroll amount in pixels.",
			},
		},
	}
}
func (t *BrowserScrollTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	_ = ctx
	var payload scrollArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
	}
	if err := t.mgr.Scroll(tc.SessionID, payload.Direction, payload.Amount); err != nil {
		return "", err
	}
	return "ok", nil
}
