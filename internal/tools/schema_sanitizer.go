// Package tools — schema sanitizer for strict backends (llama.cpp, Ollama, etc.).
// Port of Python tools/schema_sanitizer.py (lines 37-186).
//
// Fixes JSON Schema constructs that cloud APIs accept but local backends reject:
//   - Bare string "object" → {"type": "object", "properties": {}}
//   - "type": ["string","null"] → "type": "string" + nullable:true
//   - Missing properties on object nodes
//   - Invalid required entries
package tools

// SanitizeToolSchemas returns a copy of schemas with strict-backend fixes applied.
// The original registry entries are unmodified.
func SanitizeToolSchemas(schemas []ToolDefinition) []ToolDefinition {
	if len(schemas) == 0 {
		return schemas
	}

	result := make([]ToolDefinition, len(schemas))
	for i, s := range schemas {
		result[i] = s // shallow copy — Function.Parameters will be replaced
		if params, ok := s.Function.Parameters.(map[string]interface{}); ok {
			result[i].Function.Parameters = sanitizeNode(params)
		} else if _, ok := s.Function.Parameters.(string); ok {
			// Bare string — wrap into object
			result[i].Function.Parameters = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
	}
	return result
}

// sanitizeNode returns a deep copy of node with strict-backend fixes applied.
// Only modifies constructs that strict backends reject — preserves valid schemas.
func sanitizeNode(node map[string]interface{}) map[string]interface{} {
	// Deep copy so we don't mutate originals
	clean := make(map[string]interface{}, len(node))
	for k, v := range node {
		clean[k] = v
	}

	// Fix 1: array types ["string","null"] → single type + nullable
	if typeArr, ok := clean["type"].([]interface{}); ok {
		hasNull := false
		types := make([]string, 0, len(typeArr))
		for _, t := range typeArr {
			if s, ok := t.(string); ok {
				if s == "null" {
					hasNull = true
				} else {
					types = append(types, s)
				}
			}
		}
		if hasNull && len(types) == 1 {
			clean["type"] = types[0]
			clean["nullable"] = true
		}
	}

	// Fix 2: inject empty properties into object nodes missing them
	if typeStr, ok := clean["type"].(string); ok && typeStr == "object" {
		if _, hasProps := clean["properties"]; !hasProps {
			clean["properties"] = map[string]interface{}{}
		}
	}

	// Fix 3: prune required entries not in properties
	if required, ok := clean["required"].([]interface{}); ok {
		if props, ok := clean["properties"].(map[string]interface{}); ok {
			filtered := make([]interface{}, 0, len(required))
			for _, r := range required {
				if name, ok := r.(string); ok {
					if _, exists := props[name]; exists {
						filtered = append(filtered, r)
					}
				}
			}
			clean["required"] = filtered
		}
	}

	// Recurse into nested schemas (properties, items, additionalProperties only)
	// Python intentionally does NOT recurse into anyOf/oneOf/allOf to avoid
	// overwriting references shared across sibling tools
	for _, key := range []string{"properties", "items", "additionalProperties"} {
		if child, ok := clean[key].(map[string]interface{}); ok {
			clean[key] = sanitizeNode(child)
		}
	}

	return clean
}
