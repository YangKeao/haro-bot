package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

const (
	defaultBrowserTimeout  = 30 * time.Second
	defaultBrowserMaxChars = 20000
)

type BrowserManager struct {
	mu       sync.Mutex
	pw       *playwright.Playwright
	browser  playwright.Browser
	sessions map[int64]*browserSession
}

type browserSession struct {
	mu      sync.Mutex
	context playwright.BrowserContext
	page    playwright.Page
}

type pageState struct {
	URL      string        `json:"url"`
	Title    string        `json:"title"`
	Content  string        `json:"content"`
	Elements []pageElement `json:"elements"`
	Viewport pageViewport  `json:"viewport,omitempty"`
}

type pageViewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type pageElement struct {
	ID          int         `json:"id"`
	Tag         string      `json:"tag,omitempty"`
	Text        string      `json:"text,omitempty"`
	Role        string      `json:"role,omitempty"`
	Type        string      `json:"type,omitempty"`
	Placeholder string      `json:"placeholder,omitempty"`
	Href        string      `json:"href,omitempty"`
	Rect        elementRect `json:"rect,omitempty"`
}

type elementRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func NewBrowserManager() *BrowserManager {
	return &BrowserManager{sessions: make(map[int64]*browserSession)}
}

func (m *BrowserManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sess := range m.sessions {
		_ = sess.page.Close()
		_ = sess.context.Close()
	}
	m.sessions = make(map[int64]*browserSession)
	if m.browser != nil {
		_ = m.browser.Close()
		m.browser = nil
	}
	if m.pw != nil {
		_ = m.pw.Stop()
		m.pw = nil
	}
}

func (m *BrowserManager) getSession(sessionID int64) (*browserSession, error) {
	if sessionID <= 0 {
		return nil, errors.New("session_id required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[int64]*browserSession)
	}
	if sess, ok := m.sessions[sessionID]; ok {
		return sess, nil
	}
	if err := m.ensureBrowser(); err != nil {
		return nil, err
	}
	ctxObj, err := m.browser.NewContext(playwright.BrowserNewContextOptions{})
	if err != nil {
		return nil, err
	}
	page, err := ctxObj.NewPage()
	if err != nil {
		_ = ctxObj.Close()
		return nil, err
	}
	_ = page.SetViewportSize(1280, 720)
	page.SetDefaultTimeout(float64(defaultBrowserTimeout.Milliseconds()))
	page.SetDefaultNavigationTimeout(float64(defaultBrowserTimeout.Milliseconds()))
	sess := &browserSession{context: ctxObj, page: page}
	m.sessions[sessionID] = sess
	return sess, nil
}

func (m *BrowserManager) ensureBrowser() error {
	if m.browser != nil && m.pw != nil {
		return nil
	}
	pw, err := startPlaywright()
	if err != nil {
		return err
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		_ = pw.Stop()
		return err
	}
	m.pw = pw
	m.browser = browser
	return nil
}

func (m *BrowserManager) Goto(ctx context.Context, sessionID int64, targetURL string, waitMS int, timeout time.Duration) (pageState, error) {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return pageState{}, err
	}
	if err := validateURL(targetURL); err != nil {
		return pageState{}, err
	}
	if timeout <= 0 {
		timeout = defaultBrowserTimeout
	}
	sess.mu.Lock()
	sess.page.SetDefaultTimeout(float64(timeout.Milliseconds()))
	_, err = sess.page.Goto(targetURL, playwright.PageGotoOptions{
		Timeout: playwright.Float(float64(timeout.Milliseconds())),
	})
	if err == nil && waitMS > 0 {
		err = sleepWithContext(ctx, time.Duration(waitMS)*time.Millisecond)
	}
	sess.mu.Unlock()
	if err != nil {
		return pageState{}, err
	}
	return m.GetPageState(sessionID, defaultBrowserMaxChars)
}

func (m *BrowserManager) GoBack(ctx context.Context, sessionID int64, waitMS int, timeout time.Duration) (pageState, error) {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return pageState{}, err
	}
	if timeout <= 0 {
		timeout = defaultBrowserTimeout
	}
	sess.mu.Lock()
	sess.page.SetDefaultTimeout(float64(timeout.Milliseconds()))
	_, err = sess.page.GoBack(playwright.PageGoBackOptions{
		Timeout: playwright.Float(float64(timeout.Milliseconds())),
	})
	if err == nil && waitMS > 0 {
		err = sleepWithContext(ctx, time.Duration(waitMS)*time.Millisecond)
	}
	sess.mu.Unlock()
	if err != nil {
		return pageState{}, err
	}
	return m.GetPageState(sessionID, defaultBrowserMaxChars)
}

func (m *BrowserManager) GetPageState(sessionID int64, maxChars int) (pageState, error) {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return pageState{}, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	raw, err := sess.page.Evaluate(pageStateScript)
	if err != nil {
		return pageState{}, err
	}
	state, err := decodePageState(raw)
	if err != nil {
		return pageState{}, err
	}
	if maxChars <= 0 {
		maxChars = defaultBrowserMaxChars
	}
	state.Content = truncateString(state.Content, maxChars)
	return state, nil
}

func (m *BrowserManager) TakeScreenshot(sessionID int64, fullPage bool) (string, pageState, error) {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return "", pageState{}, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	raw, err := sess.page.Evaluate(pageStateScript)
	if err != nil {
		return "", pageState{}, err
	}
	state, err := decodePageState(raw)
	if err != nil {
		return "", pageState{}, err
	}
	if _, err := sess.page.Evaluate(overlayScript); err != nil {
		return "", pageState{}, err
	}
	bytes, err := sess.page.Screenshot(playwright.PageScreenshotOptions{
		FullPage: playwright.Bool(fullPage),
	})
	_, _ = sess.page.Evaluate(removeOverlayScript)
	if err != nil {
		return "", pageState{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(bytes)
	return encoded, state, nil
}

func (m *BrowserManager) Click(sessionID int64, elementID int) error {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	if elementID <= 0 {
		return errors.New("element_id required")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	selector := fmt.Sprintf("[data-haro-id=\"%d\"]", elementID)
	return sess.page.Locator(selector).Click()
}

func (m *BrowserManager) FillText(sessionID int64, elementID int, text string) error {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	if elementID <= 0 {
		return errors.New("element_id required")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	selector := fmt.Sprintf("[data-haro-id=\"%d\"]", elementID)
	return sess.page.Locator(selector).Fill(text)
}

func (m *BrowserManager) PressKey(sessionID int64, key string) error {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key required")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.page.Keyboard().Press(key)
}

func (m *BrowserManager) Scroll(sessionID int64, direction string, amount int) error {
	sess, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	if amount == 0 {
		amount = 400
	}
	dir := strings.ToLower(strings.TrimSpace(direction))
	switch dir {
	case "", "down":
		// keep amount positive
	case "up":
		amount = -amount
	default:
		return errors.New("direction must be up or down")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	_, err = sess.page.Evaluate(fmt.Sprintf("window.scrollBy(0, %d)", amount))
	return err
}

func decodePageState(raw any) (pageState, error) {
	buf, err := json.Marshal(raw)
	if err != nil {
		return pageState{}, err
	}
	var state pageState
	if err := json.Unmarshal(buf, &state); err != nil {
		return pageState{}, err
	}
	return state, nil
}

func validateURL(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("url required")
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("unsupported url scheme")
	}
	return nil
}

func truncateString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func startPlaywright() (*playwright.Playwright, error) {
	pw, err := playwright.Run()
	if err == nil {
		return pw, nil
	}
	installErr := playwright.Install(&playwright.RunOptions{
		Browsers: []string{"chromium"},
		Verbose:  false,
	})
	if installErr != nil {
		return nil, fmt.Errorf("playwright install failed: %w", installErr)
	}
	return playwright.Run()
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

const pageStateScript = `(() => {
  const isVisible = (el) => {
    const style = window.getComputedStyle(el);
    if (!style) return false;
    if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  };
  const interactive = Array.from(document.querySelectorAll(
    'a,button,input,textarea,select,[role="button"],[role="link"],[role="textbox"],[contenteditable="true"],[onclick]'
  ));
  const old = document.querySelectorAll('[data-haro-id]');
  old.forEach(el => el.removeAttribute('data-haro-id'));
  let id = 1;
  const elements = [];
  for (const el of interactive) {
    if (!isVisible(el)) continue;
    el.setAttribute('data-haro-id', String(id));
    const rect = el.getBoundingClientRect();
    const text = (el.innerText || el.value || el.getAttribute('aria-label') || el.getAttribute('alt') || '').trim();
    elements.push({
      id,
      tag: (el.tagName || '').toLowerCase(),
      text,
      role: el.getAttribute('role') || '',
      type: el.getAttribute('type') || '',
      placeholder: el.getAttribute('placeholder') || '',
      href: el.getAttribute('href') || '',
      rect: {
        x: Math.round(rect.x),
        y: Math.round(rect.y),
        w: Math.round(rect.width),
        h: Math.round(rect.height),
      },
    });
    id++;
  }
  const bodyText = document.body ? (document.body.innerText || '') : '';
  const viewport = { width: window.innerWidth || 0, height: window.innerHeight || 0 };
  return { url: location.href, title: document.title || '', content: bodyText, elements, viewport };
})()`

const overlayScript = `(() => {
  const overlayId = '__haro_overlay__';
  const existing = document.getElementById(overlayId);
  if (existing) existing.remove();
  const container = document.createElement('div');
  container.id = overlayId;
  container.style.position = 'fixed';
  container.style.left = '0';
  container.style.top = '0';
  container.style.width = '100%';
  container.style.height = '100%';
  container.style.zIndex = '2147483647';
  container.style.pointerEvents = 'none';
  document.body.appendChild(container);
  const elements = Array.from(document.querySelectorAll('[data-haro-id]'));
  for (const el of elements) {
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) continue;
    const box = document.createElement('div');
    box.style.position = 'fixed';
    box.style.left = rect.x + 'px';
    box.style.top = rect.y + 'px';
    box.style.width = rect.width + 'px';
    box.style.height = rect.height + 'px';
    box.style.border = '2px solid #ff3b30';
    box.style.boxSizing = 'border-box';
    box.style.pointerEvents = 'none';
    const label = document.createElement('div');
    label.textContent = el.getAttribute('data-haro-id') || '';
    label.style.position = 'absolute';
    label.style.left = '0';
    label.style.top = '0';
    label.style.background = '#ff3b30';
    label.style.color = '#fff';
    label.style.fontSize = '12px';
    label.style.padding = '2px 4px';
    label.style.fontFamily = 'monospace';
    box.appendChild(label);
    container.appendChild(box);
  }
})()`

const removeOverlayScript = `(() => {
  const existing = document.getElementById('__haro_overlay__');
  if (existing) existing.remove();
})()`

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
