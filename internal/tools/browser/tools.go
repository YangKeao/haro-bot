package browser

import (
	"context"
	"encoding/json"
	"time"

	"github.com/YangKeao/haro-bot/internal/tools"
)

// Browser tool type definitions

type GotoTool struct{ mgr *Manager }
type GoBackTool struct{ mgr *Manager }
type GetPageStateTool struct{ mgr *Manager }
type TakeScreenshotTool struct{ mgr *Manager }
type ClickTool struct{ mgr *Manager }
type FillTextTool struct{ mgr *Manager }
type PressKeyTool struct{ mgr *Manager }
type ScrollTool struct{ mgr *Manager }

func NewGotoTool(mgr *Manager) *GotoTool { return &GotoTool{mgr: mgr} }
func NewGoBackTool(mgr *Manager) *GoBackTool {
	return &GoBackTool{mgr: mgr}
}
func NewGetPageStateTool(mgr *Manager) *GetPageStateTool {
	return &GetPageStateTool{mgr: mgr}
}
func NewTakeScreenshotTool(mgr *Manager) *TakeScreenshotTool {
	return &TakeScreenshotTool{mgr: mgr}
}
func NewClickTool(mgr *Manager) *ClickTool { return &ClickTool{mgr: mgr} }
func NewFillTextTool(mgr *Manager) *FillTextTool {
	return &FillTextTool{mgr: mgr}
}
func NewPressKeyTool(mgr *Manager) *PressKeyTool {
	return &PressKeyTool{mgr: mgr}
}
func NewScrollTool(mgr *Manager) *ScrollTool {
	return &ScrollTool{mgr: mgr}
}

// GotoTool

type gotoArgs struct {
	URL       string `json:"url"`
	WaitMS    int    `json:"wait_ms"`
	TimeoutMS int    `json:"timeout_ms"`
}

func (t *GotoTool) Name() string        { return "browser_goto" }
func (t *GotoTool) Description() string { return "Navigate to a URL in the browser." }
func (t *GotoTool) Parameters() map[string]any {
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
func (t *GotoTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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

// GoBackTool

type goBackArgs struct {
	WaitMS    int `json:"wait_ms"`
	TimeoutMS int `json:"timeout_ms"`
}

func (t *GoBackTool) Name() string        { return "browser_go_back" }
func (t *GoBackTool) Description() string { return "Go back to the previous page." }
func (t *GoBackTool) Parameters() map[string]any {
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
func (t *GoBackTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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

// GetPageStateTool

type getPageStateArgs struct {
	MaxChars int `json:"max_chars"`
}

func (t *GetPageStateTool) Name() string { return "browser_get_page_state" }
func (t *GetPageStateTool) Description() string {
	return "Extract a simplified page state with interactive element IDs."
}
func (t *GetPageStateTool) Parameters() map[string]any {
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
func (t *GetPageStateTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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

// TakeScreenshotTool

type takeScreenshotArgs struct {
	FullPage bool `json:"full_page"`
}

type takeScreenshotResult struct {
	ContentType string    `json:"content_type"`
	Data        string    `json:"data"`
	State       pageState `json:"state"`
}

func (t *TakeScreenshotTool) Name() string { return "browser_take_screenshot" }
func (t *TakeScreenshotTool) Description() string {
	return "Take a screenshot with interactive element IDs annotated."
}
func (t *TakeScreenshotTool) Parameters() map[string]any {
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
func (t *TakeScreenshotTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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

// ClickTool

type clickArgs struct {
	ElementID int `json:"element_id"`
}

func (t *ClickTool) Name() string { return "browser_click" }
func (t *ClickTool) Description() string {
	return "Click an element by element_id from get_page_state."
}
func (t *ClickTool) Parameters() map[string]any {
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
func (t *ClickTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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

// FillTextTool

type fillTextArgs struct {
	ElementID int    `json:"element_id"`
	Text      string `json:"text"`
}

func (t *FillTextTool) Name() string { return "browser_fill_text" }
func (t *FillTextTool) Description() string {
	return "Fill text into an input element by element_id."
}
func (t *FillTextTool) Parameters() map[string]any {
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
func (t *FillTextTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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

// PressKeyTool

type pressKeyArgs struct {
	Key string `json:"key"`
}

func (t *PressKeyTool) Name() string        { return "browser_press_key" }
func (t *PressKeyTool) Description() string { return "Press a keyboard key." }
func (t *PressKeyTool) Parameters() map[string]any {
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
func (t *PressKeyTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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

// ScrollTool

type scrollArgs struct {
	Direction string `json:"direction"`
	Amount    int    `json:"amount"`
}

func (t *ScrollTool) Name() string { return "browser_scroll" }
func (t *ScrollTool) Description() string {
	return "Scroll the page up or down by a pixel amount."
}
func (t *ScrollTool) Parameters() map[string]any {
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
func (t *ScrollTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
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
