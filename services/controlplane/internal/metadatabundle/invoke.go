package metadatabundle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/storage"
)

type simpleJSONSchema struct {
	Type                 string                           `json:"type"`
	Properties           map[string]simpleJSONSchemaField `json:"properties"`
	Required             []string                         `json:"required"`
	AdditionalProperties *bool                            `json:"additionalProperties"`
}

type simpleJSONSchemaField struct {
	Type string `json:"type"`
}

func (s *Service) InvokeAutomation(ctx context.Context, actor audit.Actor, automationID string, input []byte) (AutomationInvokeResult, error) {
	bundle := s.Active()
	if bundle == nil || bundle.DebugTools == nil {
		return AutomationInvokeResult{}, ErrNotFound
	}
	doc, ok := bundle.DebugTools.Automations[strings.TrimSpace(automationID)]
	if !ok {
		return AutomationInvokeResult{}, ErrNotFound
	}
	payload, err := validateAutomationInput(doc, input)
	if err != nil {
		return AutomationInvokeResult{}, err
	}
	output, err := s.executeAutomation(ctx, actor, doc, payload)
	if err != nil {
		return AutomationInvokeResult{}, err
	}
	if err := validateAgainstSchema(doc.OutputSchemaPath, output); err != nil {
		return AutomationInvokeResult{}, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, actor, audit.Event{
			Action:     "automation.invoke",
			TargetType: "automation",
			TargetID:   automationID,
			Outcome:    storage.AuditOutcomeSuccess,
			Details: map[string]any{
				"documentVersion": doc.Document.Version,
				"metadataVersion": bundle.Manifest.Metadata.MetadataVersion,
			},
		})
	}
	return AutomationInvokeResult{
		AutomationID:    automationID,
		DocumentVersion: doc.Document.Version,
		MetadataVersion: bundle.Manifest.Metadata.MetadataVersion,
		Output:          output,
	}, nil
}

func validateAutomationInput(doc AutomationDocument, input []byte) (map[string]any, error) {
	if len(input) == 0 {
		input = []byte(`{}`)
	}
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("metadatabundle: invalid automation input: %w", err)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if err := validateAgainstSchema(doc.InputSchemaPath, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func validateAgainstSchema(schemaPath string, payload map[string]any) error {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("metadatabundle: read schema %s: %w", schemaPath, err)
	}
	var schema simpleJSONSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("metadatabundle: parse schema %s: %w", schemaPath, err)
	}
	if schema.Type != "" && schema.Type != "object" {
		return fmt.Errorf("metadatabundle: unsupported schema type %q in %s", schema.Type, schemaPath)
	}
	required := map[string]struct{}{}
	for _, key := range schema.Required {
		required[strings.TrimSpace(key)] = struct{}{}
	}
	for key := range required {
		if _, ok := payload[key]; !ok {
			return fmt.Errorf("metadatabundle: missing required field %q", key)
		}
	}
	allowExtra := true
	if schema.AdditionalProperties != nil {
		allowExtra = *schema.AdditionalProperties
	}
	for key, value := range payload {
		field, ok := schema.Properties[key]
		if !ok {
			if !allowExtra {
				return fmt.Errorf("metadatabundle: unexpected field %q", key)
			}
			continue
		}
		if err := validateFieldType(key, field.Type, value); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldType(key, want string, value any) error {
	switch want {
	case "", "object":
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("metadatabundle: field %q must be a string", key)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("metadatabundle: field %q must be a boolean", key)
		}
	case "number":
		switch value.(type) {
		case float64, float32, int, int32, int64:
			return nil
		default:
			return fmt.Errorf("metadatabundle: field %q must be a number", key)
		}
	default:
		return fmt.Errorf("metadatabundle: unsupported field type %q for %q", want, key)
	}
	return nil
}

func (s *Service) executeAutomation(ctx context.Context, actor audit.Actor, doc AutomationDocument, input map[string]any) (map[string]any, error) {
	if len(doc.Do) != 1 {
		return nil, fmt.Errorf("metadatabundle: automation %q has unsupported step count", doc.ID)
	}
	step := doc.Do[0]
	switch strings.TrimSpace(step.Call.Function) {
	case "zon:api:audit.export.create":
		if s.auditOps == nil {
			return nil, fmt.Errorf("metadatabundle: audit export action is unavailable")
		}
		ownerID := strings.TrimSpace(actor.UserID)
		if ownerID == "" {
			ownerID = "system"
		}
		op, err := s.auditOps.StartExport(ctx, ownerID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"exportId": op.ID,
			"status":   string(op.Status),
		}, nil
	default:
		return nil, fmt.Errorf("metadatabundle: unsupported automation function %q", step.Call.Function)
	}
}
