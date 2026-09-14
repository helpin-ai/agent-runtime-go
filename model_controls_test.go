package sdk

import (
	"encoding/json"
	"testing"
)

func TestRunModelControlsDistinguishAbsentAndEmpty(t *testing.T) {
	for _, raw := range []string{`{"provider":"openai","model":"custom"}`, `{"provider":"openai","model":"custom","controls":{}}`} {
		var model RunModel
		if err := json.Unmarshal([]byte(raw), &model); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != raw {
			t.Fatalf("lost control override: %s", encoded)
		}
	}
}

func TestValidateRunModelControls(t *testing.T) {
	high, fast, invalid := "high", "fast", "unknown"
	for _, tc := range []struct {
		name, provider string
		controls       ModelControls
		wantErr        bool
	}{
		{"openai", "openai", ModelControls{ReasoningEffort: &high, ServiceTier: &fast}, false},
		{"chatgpt", "openai_chatgpt", ModelControls{ReasoningEffort: &high}, false},
		{"anthropic reasoning", "anthropic", ModelControls{ReasoningEffort: &high}, true},
		{"openrouter service tier", "openrouter", ModelControls{ServiceTier: &fast}, true},
		{"unknown effort", "openai", ModelControls{ReasoningEffort: &invalid}, true},
		{"wrong routing controls", "anthropic", ModelControls{OpenRouter: &OpenRouterModelControls{}}, true},
		{"unpriced custom", "anthropic", ModelControls{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRunModel(&RunModel{Provider: tc.provider, Model: "custom-without-price", Controls: &tc.controls})
			if (err != nil) != tc.wantErr {
				t.Fatalf("validation=%v", err)
			}
		})
	}
}
