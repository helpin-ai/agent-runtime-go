package sdk

import "testing"

func TestCompatibleEndpointValidation(t *testing.T) {
	endpoint := ModelEndpoint{ID: "local-model", BaseURL: "http://127.0.0.1:8081/v1", AuthMode: "none"}
	model := RunModel{Provider: "openai_compatible", Model: "custom-model", Endpoint: &endpoint, Controls: &ModelControls{}}
	if err := ValidateRunModel(&model); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"https://example.com/v1/", "https://user:secret@example.com/v1", "https://example.com/v1?q=secret", "https://example.com/v1#fragment", "https://example.com/a/../v1", "https://example.com/%2Fv1", "file:///tmp/model"} {
		copy := endpoint
		copy.BaseURL = value
		if err := ValidateModelEndpoint(&copy); err == nil {
			t.Errorf("accepted unsafe URL: %s", value)
		}
	}
	model.Provider = "openai"
	if err := ValidateRunModel(&model); err == nil {
		t.Fatal("endpoint accepted on fixed provider")
	}
	model.Provider, model.Endpoint = "openai_compatible", nil
	if err := ValidateRunModel(&model); err == nil {
		t.Fatal("missing explicit endpoint accepted")
	}
	model.Endpoint = &endpoint
	effort := "high"
	model.Controls.ReasoningEffort = &effort
	if err := ValidateRunModel(&model); err == nil {
		t.Fatal("unsupported compatible controls accepted")
	}
}
