package agentplan

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
)

func statusSchema() map[string]any {
	return map[string]any{"type": "string", "enum": []string{string(StatusNotStarted), string(StatusInProgress), string(StatusBlocked), string(StatusComplete), string(StatusCancelled)}}
}

// Validate before opening the store: malformed calls must not create workspaces.
// Errors identify fields and constraints, never caller-supplied values.
func (s *MCPServer) validateArguments(ctx context.Context, name string, args map[string]any) error {
	for _, tool := range s.Tools(ctx) {
		if tool.Name != name {
			continue
		}
		properties := tool.InputSchema["properties"].(map[string]any)
		required, _ := tool.InputSchema["required"].([]string)
		for _, key := range required {
			if _, ok := args[key]; !ok {
				return fmt.Errorf("required parameter %q is missing", key)
			}
		}
		for key, value := range args {
			raw, ok := properties[key]
			if !ok {
				return fmt.Errorf("unknown parameter %q", key)
			}
			if err := validateProperty(key, value, raw.(map[string]any)); err != nil {
				return err
			}
		}
		if err := validateRequiredAlternative(tool.InputSchema, args); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown tool; use tools/list to inspect available tools")
}

func validateProperty(key string, value any, property map[string]any) error {
	valid := false
	switch property["type"] {
	case "string":
		_, valid = value.(string)
	case "boolean":
		_, valid = value.(bool)
	case "integer":
		switch number := value.(type) {
		case int:
			valid = true
		case float64:
			valid = math.Trunc(number) == number
		}
	case "object":
		_, valid = value.(map[string]any)
	case "array":
		if value != nil && reflect.TypeOf(value).Kind() == reflect.Slice {
			valid = true
			values := reflect.ValueOf(value)
			for i := 0; i < values.Len(); i++ {
				if err := validateProperty(key, values.Index(i).Interface(), property["items"].(map[string]any)); err != nil {
					return err
				}
			}
		}
	}
	if !valid {
		return fmt.Errorf("parameter %q must have type %s", key, property["type"])
	}
	if minimum, ok := property["minLength"].(int); ok && len(value.(string)) < minimum {
		return fmt.Errorf("parameter %q must contain at least %d characters", key, minimum)
	}
	if minimum, ok := property["minItems"].(int); ok && reflect.ValueOf(value).Len() < minimum {
		return fmt.Errorf("parameter %q must contain at least %d items", key, minimum)
	}
	if choices, ok := property["enum"].([]string); ok && !slices.Contains(choices, value.(string)) {
		return fmt.Errorf("parameter %q must be one of %v", key, choices)
	}
	return nil
}

func validateRequiredAlternative(schema map[string]any, args map[string]any) error {
	alternatives, ok := schema["anyOf"].([]any)
	if !ok {
		return nil
	}
	parameterNames := make([]string, 0, len(alternatives))
	for _, raw := range alternatives {
		alternative, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		required, ok := alternative["required"].([]string)
		if !ok || len(required) == 0 {
			continue
		}
		matched := true
		for _, key := range required {
			if _, exists := args[key]; !exists {
				matched = false
				break
			}
		}
		if matched {
			return nil
		}
		parameterNames = append(parameterNames, required...)
	}
	return invalidArgumentError(
		fmt.Errorf("at least one of parameters %q is required", parameterNames),
	)
}
