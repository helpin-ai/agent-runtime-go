package sdk

import (
	"fmt"
	"strings"
)

// ModelControls contains only model request options, not execution or tool limits.
type ModelControls struct {
	ReasoningEffort *string                  `json:"reasoning_effort,omitempty"`
	ServiceTier     *string                  `json:"service_tier,omitempty"`
	OpenRouter      *OpenRouterModelControls `json:"openrouter,omitempty"`
}

// OpenRouterModelControls selects provider routing preferences.
type OpenRouterModelControls struct {
	Provider *OpenRouterProviderPreferences `json:"provider,omitempty"`
}

// OpenRouterProviderPreferences restricts the selected provider's quantization.
type OpenRouterProviderPreferences struct {
	Quantizations []string `json:"quantizations,omitempty"`
}

// ReasoningEfforts returns the structurally supported values. Runtime capability
// validation still decides whether a particular execution path supports them.
func ReasoningEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh"}
}

// ValidateModelControls is the shared host/runtime validator for request controls.
func ValidateModelControls(provider string, controls ModelControls) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	effort := modelControlValue(controls.ReasoningEffort)
	if effort != "" {
		switch effort {
		case "none", "minimal", "low", "medium", "high", "xhigh":
		default:
			return fmt.Errorf("reasoning_effort must be one of %s", strings.Join(ReasoningEfforts(), ", "))
		}
		switch provider {
		case "openai", "openai_chatgpt", "openrouter", "openrouter_responses":
		default:
			return fmt.Errorf("reasoning_effort requires an OpenAI Responses provider")
		}
	}
	tier := modelControlValue(controls.ServiceTier)
	if tier != "" {
		switch tier {
		case "standard", "fast", "auto", "default", "flex", "priority":
		default:
			return fmt.Errorf("service_tier must be one of standard, fast, flex, auto, default, priority")
		}
		if provider != "openai" && provider != "openai_chatgpt" {
			return fmt.Errorf("service_tier is only supported for provider openai or openai_chatgpt")
		}
	}
	if controls.OpenRouter != nil && provider != "openrouter" && provider != "openrouter_responses" {
		return fmt.Errorf("openrouter is only supported for provider openrouter")
	}
	return nil
}

// ValidateRunModel checks a concrete route and any explicit model controls.
// Hosts may represent custom model names; price catalogs do not belong here.
func ValidateRunModel(model *RunModel) error {
	if model == nil {
		return nil
	}
	switch model.Provider {
	case "openai", "anthropic", "openrouter", "openrouter_responses", "openai_chatgpt":
	default:
		return fmt.Errorf("unsupported model provider")
	}
	if strings.TrimSpace(model.Model) == "" || len(model.Model) > 256 {
		return fmt.Errorf("model.model is required and must not exceed 256 characters")
	}
	if model.Controls != nil {
		return ValidateModelControls(model.Provider, *model.Controls)
	}
	return nil
}

func modelControlValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*value))
}
