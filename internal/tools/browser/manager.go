package browser

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

type Manager struct {
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

func NewManager() *Manager {
	return &Manager{sessions: make(map[int64]*browserSession)}
}

func (m *Manager) Close() {
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

func (m *Manager) getSession(sessionID int64) (*browserSession, error) {
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

func (m *Manager) ensureBrowser() error {
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

func (m *Manager) Goto(ctx context.Context, sessionID int64, targetURL string, waitMS int, timeout time.Duration) (pageState, error) {
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

func (m *Manager) GoBack(ctx context.Context, sessionID int64, waitMS int, timeout time.Duration) (pageState, error) {
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

func (m *Manager) GetPageState(sessionID int64, maxChars int) (pageState, error) {
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

func (m *Manager) TakeScreenshot(sessionID int64, fullPage bool) (string, pageState, error) {
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

func (m *Manager) Click(sessionID int64, elementID int) error {
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

func (m *Manager) FillText(sessionID int64, elementID int, text string) error {
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

func (m *Manager) PressKey(sessionID int64, key string) error {
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

func (m *Manager) Scroll(sessionID int64, direction string, amount int) error {
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
  const IGNORED_TAGS = new Set(['script', 'style', 'noscript', 'svg', 'canvas', 'iframe', 'template']);
  const NOISE_CONTAINER_TAGS = new Set(['nav', 'footer', 'header', 'aside']);
  const PRESENTATION_ROLES = new Set(['presentation', 'none']);
  const GENERIC_CONTAINER_TAGS = new Set(['div', 'span', 'section', 'main', 'article']);
  const isVisible = (el) => {
    const style = window.getComputedStyle(el);
    if (!style) return false;
    if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  };

  const isDisabled = (el) => {
    if (!el) return false;
    if (el.disabled) return true;
    const ariaDisabled = el.getAttribute && el.getAttribute('aria-disabled');
    return ariaDisabled === 'true';
  };

  const isHiddenLike = (el) => {
    if (!el) return true;
    if (el.hidden) return true;
    if (el.getAttribute && el.getAttribute('aria-hidden') === 'true') return true;
    const tag = (el.tagName || '').toLowerCase();
    if (IGNORED_TAGS.has(tag)) return true;
    const role = (el.getAttribute && el.getAttribute('role')) || '';
    if (PRESENTATION_ROLES.has(role)) return true;
    return false;
  };

  const isNoiseContainer = (el) => {
    if (!el) return false;
    const tag = (el.tagName || '').toLowerCase();
    if (NOISE_CONTAINER_TAGS.has(tag)) return true;
    const role = (el.getAttribute && el.getAttribute('role')) || '';
    if (role === 'navigation' || role === 'banner' || role === 'contentinfo' || role === 'complementary') {
      return true;
    }
    return false;
  };

  const hasIgnoredAncestor = (el, predicate) => {
    let cur = el;
    while (cur) {
      if (predicate(cur)) return true;
      cur = cur.parentElement;
    }
    return false;
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
    if (isHiddenLike(el)) continue;
    if (isDisabled(el)) continue;
    const tag = (el.tagName || '').toLowerCase();
    const text = (el.innerText || el.value || el.getAttribute('aria-label') || el.getAttribute('alt') || el.getAttribute('title') || '').trim();
    const placeholder = (el.getAttribute && el.getAttribute('placeholder')) || '';
    const href = (el.getAttribute && el.getAttribute('href')) || '';
    const role = (el.getAttribute && el.getAttribute('role')) || '';
    const type = (el.getAttribute && el.getAttribute('type')) || '';
    const hasLabel = text !== '' || placeholder !== '';
    const isFormControl = tag === 'input' || tag === 'textarea' || tag === 'select' || tag === 'button';
    if (GENERIC_CONTAINER_TAGS.has(tag) && !hasLabel && !href && !isFormControl) {
      continue;
    }
    el.setAttribute('data-haro-id', String(id));
    const rect = el.getBoundingClientRect();
    elements.push({
      id,
      tag,
      text,
      role,
      type,
      placeholder,
      href,
      rect: {
        x: Math.round(rect.x),
        y: Math.round(rect.y),
        w: Math.round(rect.width),
        h: Math.round(rect.height),
      },
    });
    id++;
  }

  const content = (() => {
    if (!document.body) return '';
    const parts = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, {
      acceptNode: (node) => {
        const value = (node.nodeValue || '').trim();
        if (!value) return NodeFilter.FILTER_REJECT;
        const parent = node.parentElement;
        if (!parent) return NodeFilter.FILTER_REJECT;
        if (hasIgnoredAncestor(parent, isHiddenLike)) return NodeFilter.FILTER_REJECT;
        if (hasIgnoredAncestor(parent, isNoiseContainer)) return NodeFilter.FILTER_REJECT;
        return NodeFilter.FILTER_ACCEPT;
      },
    });
    while (walker.nextNode()) {
      const value = (walker.currentNode && walker.currentNode.nodeValue || '').trim();
      if (value) parts.push(value);
    }
    return parts.join('\n');
  })();

  const viewport = { width: window.innerWidth || 0, height: window.innerHeight || 0 };
  return { url: location.href, title: document.title || '', content, elements, viewport };
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
