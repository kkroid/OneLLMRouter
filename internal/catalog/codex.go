package catalog

//go:generate go run generate_codex_models.go

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// officialCodexModelsJSON is generated from openai/codex rust-v0.144.6.
//
//go:embed codex_models_0.144.6.json
var officialCodexModelsJSON []byte

var (
	officialModelsOnce sync.Once
	officialModels     map[string]json.RawMessage
	officialModelsErr  error
)

const customModelBaseInstructions = "You are Codex, a coding agent."

func MarshalCodex(models []Model) ([]byte, error) {
	templates, err := loadOfficialModels()
	if err != nil {
		return nil, err
	}
	fallbackTemplate, ok := templates["gpt-5.6-sol"]
	if !ok {
		return nil, fmt.Errorf("Codex compatibility template gpt-5.6-sol is missing")
	}

	codexModels := make([]map[string]json.RawMessage, 0, len(models))
	for priority, entry := range models {
		baseModel := baseModelID(entry.ID)
		template, knownModel := templates[baseModel]
		if !knownModel {
			template = fallbackTemplate
		}
		model, err := decodeCodexModel(template)
		if err != nil {
			return nil, fmt.Errorf("decode Codex template for %s: %w", entry.ID, err)
		}
		setCodexField(model, "availability_nux", nil)
		setCodexField(model, "additional_speed_tiers", []string{})
		setCodexField(model, "service_tiers", []any{})
		setCodexField(model, "default_service_tier", nil)
		setCodexField(model, "auto_review_model_override", nil)
		if !knownModel {
			setCodexField(model, "description", nil)
			setCodexField(model, "model_messages", nil)
			setCodexField(model, "base_instructions", customModelBaseInstructions)
		}
		if len(entry.CodexMetadata) > 0 {
			upstream, err := decodeCodexModel(entry.CodexMetadata)
			if err != nil {
				return nil, fmt.Errorf("decode upstream Codex metadata for %s: %w", entry.ID, err)
			}
			for key, value := range upstream {
				model[key] = value
			}
		}

		setCodexField(model, "slug", entry.ID)
		setCodexField(model, "display_name", entry.ID)
		setCodexField(model, "priority", priority)
		setCodexField(model, "visibility", "list")
		setCodexField(model, "supported_in_api", true)
		setCodexField(model, "prefer_websockets", false)

		switch {
		case entry.ReasoningConfigured:
			setCodexField(model, "default_reasoning_level", entry.DefaultReasoningLevel)
			setCodexField(model, "supported_reasoning_levels", entry.SupportedReasoningLevels)
		case entry.HasReasoningMetadata || entry.DefaultReasoningLevel != "" || len(entry.SupportedReasoningLevels) > 0:
			fallbackDefault, fallbackLevels := templateReasoning(model)
			if !knownModel {
				fallbackDefault, fallbackLevels = commonReasoning()
			}
			defaultLevel, levels := completeReasoning(
				entry.DefaultReasoningLevel,
				entry.SupportedReasoningLevels,
				fallbackDefault,
				fallbackLevels,
			)
			setCodexField(model, "default_reasoning_level", defaultLevel)
			setCodexField(model, "supported_reasoning_levels", levels)
		case !knownModel:
			defaultLevel, levels := commonReasoning()
			setCodexField(model, "default_reasoning_level", defaultLevel)
			setCodexField(model, "supported_reasoning_levels", levels)
		}

		codexModels = append(codexModels, model)
	}
	return json.Marshal(map[string]any{"models": codexModels})
}

func loadOfficialModels() (map[string]json.RawMessage, error) {
	officialModelsOnce.Do(func() {
		var document struct {
			Models []json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(officialCodexModelsJSON, &document); err != nil {
			officialModelsErr = fmt.Errorf("decode embedded Codex models: %w", err)
			return
		}
		officialModels = make(map[string]json.RawMessage, len(document.Models))
		for _, raw := range document.Models {
			var identity struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(raw, &identity); err != nil {
				officialModelsErr = fmt.Errorf("decode embedded Codex model identity: %w", err)
				return
			}
			officialModels[identity.Slug] = append(json.RawMessage(nil), raw...)
		}
	})
	return officialModels, officialModelsErr
}

func decodeCodexModel(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var model map[string]json.RawMessage
	if err := json.Unmarshal(raw, &model); err != nil {
		return nil, err
	}
	return model, nil
}

func setCodexField(model map[string]json.RawMessage, key string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	model[key] = data
}

func baseModelID(modelID string) string {
	_, base, found := strings.Cut(modelID, "/")
	if found {
		return base
	}
	return modelID
}

func templateReasoning(model map[string]json.RawMessage) (string, []ReasoningLevel) {
	var defaultLevel string
	_ = json.Unmarshal(model["default_reasoning_level"], &defaultLevel)
	var levels []ReasoningLevel
	_ = json.Unmarshal(model["supported_reasoning_levels"], &levels)
	return defaultLevel, levels
}

func completeReasoning(defaultLevel string, levels []ReasoningLevel, fallbackDefault string, fallbackLevels []ReasoningLevel) (string, []ReasoningLevel) {
	if len(levels) == 0 {
		levels = append([]ReasoningLevel(nil), fallbackLevels...)
	}
	if defaultLevel == "" {
		defaultLevel = preferredDefaultReasoning(fallbackDefault, levels)
	}
	return defaultLevel, levels
}

func preferredDefaultReasoning(preferred string, levels []ReasoningLevel) string {
	for _, level := range levels {
		if level.Effort == preferred {
			return preferred
		}
	}
	for _, level := range levels {
		if level.Effort == "medium" {
			return "medium"
		}
	}
	if len(levels) > 0 {
		return levels[0].Effort
	}
	return preferred
}

func commonReasoning() (string, []ReasoningLevel) {
	return "medium", reasoningLevels([]string{"low", "medium", "high", "xhigh"})
}

func reasoningLevels(efforts []string) []ReasoningLevel {
	levels := make([]ReasoningLevel, 0, len(efforts))
	for _, effort := range efforts {
		levels = append(levels, ReasoningLevel{
			Effort:      effort,
			Description: reasoningDescription(effort),
		})
	}
	return levels
}

func reasoningDescription(effort string) string {
	switch effort {
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balances speed and reasoning depth for everyday tasks"
	case "high":
		return "Greater reasoning depth for complex problems"
	case "xhigh":
		return "Extra high reasoning depth for complex problems"
	case "max":
		return "Maximum reasoning depth for the hardest problems"
	case "ultra":
		return "Maximum reasoning with automatic task delegation"
	default:
		return ""
	}
}
