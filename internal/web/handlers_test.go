package web

import (
	"bytes"
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
