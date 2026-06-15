package tests

import (
	"testing"

	"glm5.2proxy/internal/models"
	"glm5.2proxy/internal/openai"
	"glm5.2proxy/internal/state"
)

func TestModelsAndOpenAITranslation(t *testing.T) {
	turbo, ok := models.Resolve("glm-5turbo")
	if !ok || turbo.UpstreamID != "GLM-5-Turbo" || turbo.DailyTokenAllowance != 2_000_000 {
		t.Fatalf("unexpected model resolution: %+v", turbo)
	}
	body := map[string]any{
		"model": "glm-5-turbo",
		"messages": []any{
			map[string]any{"role": "system", "content": "system"},
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "description": "test", "parameters": map[string]any{"type": "object"}}}},
	}
	translated := openai.ToAnthropic(body, nil, turbo, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	if translated["model"] != "GLM-5-Turbo" {
		t.Fatalf("wrong upstream model: %+v", translated)
	}
	thinking := translated["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 32000 || translated["output_config"].(map[string]any)["effort"] != "max" {
		t.Fatalf("thinking settings missing: %+v", translated)
	}
	if len(translated["tools"].([]any)) != 1 || len(translated["messages"].([]any)) != 1 {
		t.Fatalf("message/tool translation failed: %+v", translated)
	}
}

func TestTranslationKeepsThinkingParametersValidForClientTokenLimit(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model":       "glm-5.2",
		"max_tokens":  float64(8192),
		"temperature": float64(0.2),
		"top_p":       float64(0.9),
		"messages":    []any{map[string]any{"role": "user", "content": "hello"}},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	thinking := translated["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 4096 {
		t.Fatalf("thinking budget must fit inside max_tokens: %+v", translated)
	}
	if _, ok := translated["temperature"]; ok {
		t.Fatalf("temperature must be omitted while thinking is enabled: %+v", translated)
	}
	if _, ok := translated["top_p"]; ok {
		t.Fatalf("top_p must be omitted while thinking is enabled: %+v", translated)
	}
}

func TestTranslationDisablesThinkingWhenClientTokenLimitIsTooSmall(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model":       "glm-5.2",
		"max_tokens":  float64(512),
		"temperature": float64(0.2),
		"messages":    []any{map[string]any{"role": "user", "content": "hello"}},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	if _, ok := translated["thinking"]; ok {
		t.Fatalf("thinking must be omitted when max_tokens cannot contain a valid budget: %+v", translated)
	}
	if translated["temperature"] != float64(0.2) {
		t.Fatalf("temperature should remain available without thinking: %+v", translated)
	}
}
