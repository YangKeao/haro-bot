package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/YangKeao/haro-bot/internal/skills"
)

func TestSkillSourceJSONUsesEmptyFilterArray(t *testing.T) {
	result := skillSourceJSON(skills.Source{ID: 1, Status: "active"})
	filters, ok := result["skill_filters"].([]string)
	if !ok || filters == nil || len(filters) != 0 {
		t.Fatalf("expected an empty filter array, got %#v", result["skill_filters"])
	}
}

func TestNormalizeProviderInputRetainsOrClearsAPIKey(t *testing.T) {
	key := "provider-secret"
	write, err := normalizeProviderInput(providerInput{Name: "OAuth", BaseURL: "https://example.test/v1/", APIKey: &key}, "old-secret")
	if err != nil || write.APIKey != key || write.BaseURL != "https://example.test/v1" {
		t.Fatalf("explicit API key was not used: %#v, %v", write, err)
	}
	write, err = normalizeProviderInput(providerInput{Name: "OAuth", BaseURL: "https://example.test/v1"}, "old-secret")
	if err != nil || write.APIKey != "old-secret" {
		t.Fatalf("existing API key was not retained: %#v, %v", write, err)
	}
	write, err = normalizeProviderInput(providerInput{Name: "OAuth", BaseURL: "https://example.test/v1", ClearAPIKey: true}, "old-secret")
	if err != nil || write.APIKey != "" {
		t.Fatalf("explicit API key clear was not honored: %#v, %v", write, err)
	}
}

func TestNormalizeProviderInputValidatesResponsesWebSearch(t *testing.T) {
	write, err := normalizeProviderInput(providerInput{
		Name: "OAuth", BaseURL: "https://example.test/v1",
		APIMode: "responses", WebSearchEnabled: true,
	}, "")
	if err != nil || write.APIMode != "responses" || !write.WebSearchEnabled {
		t.Fatalf("expected Responses web search provider: %#v, %v", write, err)
	}
	if _, err := normalizeProviderInput(providerInput{
		Name: "OAuth", BaseURL: "https://example.test/v1",
		APIMode: "chat_completions", WebSearchEnabled: true,
	}, ""); err == nil {
		t.Fatal("expected web search with Chat Completions to be rejected")
	}
	if _, err := normalizeProviderInput(providerInput{
		Name: "OAuth", BaseURL: "https://example.test/v1", APIMode: "unknown",
	}, ""); err == nil {
		t.Fatal("expected unknown API mode to be rejected")
	}
}

func TestNormalizeAgentInputAllowsProviderSpecificReasoning(t *testing.T) {
	effort := "XHIGH"
	result, err := normalizeAgentInput(agentInput{ProviderID: 9, Name: "Vision", Model: "vision", ReasoningEffortOverride: &effort, EffectiveContextWindowPercent: 0})
	if err != nil {
		t.Fatalf("normalize agent: %v", err)
	}
	if result.ProviderID != 9 || result.ReasoningEffortOverride == nil || *result.ReasoningEffortOverride != "xhigh" || result.EffectiveContextWindowPercent != 95 {
		t.Fatalf("unexpected normalized agent: %#v", result)
	}
}

type staticSkillLister []skills.Metadata

func (s staticSkillLister) List() []skills.Metadata { return []skills.Metadata(s) }

func TestValidateSelectedSkillsRejectsUnavailableNames(t *testing.T) {
	installed := staticSkillLister{{Name: "jellyfin"}}
	if err := validateSelectedSkills(installed, []string{" jellyfin "}); err != nil {
		t.Fatalf("expected installed skill to validate: %v", err)
	}
	if err := validateSelectedSkills(installed, []string{"agent-browser"}); err == nil || err.Error() != `skill "agent-browser" is not installed` {
		t.Fatalf("expected unavailable skill rejection, got %v", err)
	}
}

func TestCreateAgentRejectsUnavailableSkill(t *testing.T) {
	server := &Server{skills: skills.NewManager(nil, t.TempDir(), nil)}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewBufferString(`{
		"provider_id": 1,
		"name": "Agent",
		"model": "model",
		"skill_names": ["agent-browser"]
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.handleCreateAgent(response, request)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte(`"code":"invalid_agent"`)) {
		t.Fatalf("expected invalid_agent response, status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNormalizeModelCatalogAcceptsEnrichedAndStandardModels(t *testing.T) {
	models, err := normalizeModelCatalog([]byte(`{"object":"list","data":[{"id":"gpt-rich","context_window":272000,"default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low","description":"Fast"},"high"],"input_modalities":["text","image"]},{"id":"plain"}]}`))
	if err != nil {
		t.Fatalf("normalize catalog: %v", err)
	}
	if len(models) != 2 || models[0].ContextWindow != 272000 || models[0].DefaultReasoningEffort != "low" || len(models[0].ReasoningEfforts) != 2 || models[1].ID != "plain" {
		t.Fatalf("unexpected catalog: %#v", models)
	}
}

func TestReadImageValidation(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	file, err := os.CreateTemp(t.TempDir(), "image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(png); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, mimeType, err := readImage(file)
	if err != nil {
		t.Fatalf("read image: %v", err)
	}
	if mimeType != "image/png" || len(data) != len(png) {
		t.Fatalf("mime=%q len=%d", mimeType, len(data))
	}

	bad, err := os.CreateTemp(t.TempDir(), "bad-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bad.WriteString("not an image")
	_, _ = bad.Seek(0, 0)
	if _, _, err := readImage(bad); err == nil {
		t.Fatal("expected unsupported image error")
	}
}
