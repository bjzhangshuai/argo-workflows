package parameterization

import (
	"fmt"
	"math"
)

// importedSchema lowers the explicitly supported import vocabulary to draft-07.
// It operates on a copy: frozen definitions, hashes and caller values retain the
// original document. This is not general JSON Schema 2020-12 support.
func importedSchema(raw any, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("schema exceeds 64 levels")
	}
	source, ok := raw.(map[string]any)
	if !ok {
		return Clone(raw), nil
	}
	out := map[string]any{}
	for key, value := range source {
		switch key {
		case "properties", "definitions":
			children, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid schema properties")
			}
			next := map[string]any{}
			for name, child := range children {
				v, err := importedSchema(child, depth+1)
				if err != nil {
					return nil, err
				}
				next[name] = v
			}
			out[key] = next
		case "items", "prefixItems", "allOf", "anyOf", "oneOf":
			if list, ok := value.([]any); ok {
				next := []any{}
				for _, child := range list {
					v, err := importedSchema(child, depth+1)
					if err != nil {
						return nil, err
					}
					next = append(next, v)
				}
				out[key] = next
			} else {
				v, err := importedSchema(value, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = v
			}
		case "additionalProperties", "additionalItems", "not", "if", "then", "else":
			v, err := importedSchema(value, depth+1)
			if err != nil {
				return nil, err
			}
			out[key] = v
		default:
			out[key] = Clone(value)
		}
	}
	if value, present := out["allOf"]; present {
		if _, ok := value.([]any); !ok {
			return nil, fmt.Errorf("allOf must be an array")
		}
	}
	if prefix, present := out["prefixItems"]; present {
		list, ok := prefix.([]any)
		_, oldTuple := out["items"].([]any)
		_, oldTail := out["additionalItems"]
		if !ok || len(list) == 0 || oldTuple || oldTail {
			return nil, fmt.Errorf("prefixItems requires a nonempty tuple without legacy tuple keywords")
		}
		if tail, present := out["items"]; present {
			out["additionalItems"] = tail
		}
		out["items"] = list
		delete(out, "prefixItems")
	}
	if value, present := out["deprecated"]; present {
		if _, ok := value.(bool); !ok {
			return nil, fmt.Errorf("deprecated must be boolean")
		}
		delete(out, "deprecated") // annotation only; no validation semantics
	}
	if value, present := out["x-kubernetes-int-or-string"]; present {
		flag, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("x-kubernetes-int-or-string must be boolean")
		}
		if flag {
			// Intersect instead of replacing authored type/enum constraints.
			out["allOf"] = append(schemaAlternatives(out["allOf"]), map[string]any{"type": []any{"integer", "string"}})
		}
		delete(out, "x-kubernetes-int-or-string")
	}
	if format, ok := out["format"].(string); ok && (format == "ipv4" || format == "ipv4-cidr") {
		octet := `(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])`
		pattern := `^` + octet + `(\.` + octet + `){3}`
		if format == "ipv4-cidr" {
			pattern += `/(3[0-2]|[12]?[0-9])`
		}
		pattern += `$`
		out["allOf"] = append(schemaAlternatives(out["allOf"]), map[string]any{"pattern": pattern})
		delete(out, "format")
	}
	return out, nil
}

func schemaAlternatives(value any) []any {
	if value == nil {
		return nil
	}
	if list, ok := value.([]any); ok {
		return list
	}
	// Retain malformed input so draft-07 validation still rejects it.
	return []any{value}
}

// Definition bounds can describe int64/uint64 even though editable JSON values
// retain their existing safe-integer limit. Only numeric bound keywords at
// schema positions are masked for the complexity check, never data/defaults.
func schemaComplexity(raw any) error {
	var limitErr error
	count := 0
	var mask func(any, int) any
	mask = func(raw any, depth int) any {
		count++
		if depth > 64 || count > 10000 {
			limitErr = fmt.Errorf("JSON exceeds 64 levels or 10000 values")
			return nil
		}
		s, ok := raw.(map[string]any)
		if !ok {
			return raw
		}
		out := map[string]any{}
		for k, v := range s {
			switch k {
			case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
				if n, ok := v.(float64); ok && !math.IsNaN(n) && !math.IsInf(n, 0) {
					v = float64(0)
				}
			case "properties", "definitions":
				if children, ok := v.(map[string]any); ok {
					next := map[string]any{}
					for name, child := range children {
						next[name] = mask(child, depth+1)
					}
					v = next
				}
			case "items", "prefixItems", "additionalItems", "additionalProperties", "allOf", "anyOf", "oneOf", "not", "if", "then", "else":
				if list, ok := v.([]any); ok {
					next := make([]any, len(list))
					for i, child := range list {
						next[i] = mask(child, depth+1)
					}
					v = next
				} else {
					v = mask(v, depth+1)
				}
			}
			out[k] = v
		}
		return out
	}
	masked := mask(raw, 0)
	if limitErr != nil {
		return limitErr
	}
	return checkComplexity(masked, 0, new(int))
}

// ArrayItemSchema handles authored positional constraints without changing the
// original form or replacing the common tail schema with one tuple position.
func ArrayItemSchema(schema map[string]any, index int) map[string]any {
	if prefix, ok := schema["prefixItems"].([]any); ok {
		if index < 0 {
			return nil
		}
		if index < len(prefix) {
			child, _ := prefix[index].(map[string]any)
			return child
		}
	}
	if tuple, ok := schema["items"].([]any); ok {
		if index < 0 {
			return nil
		}
		if index < len(tuple) {
			child, _ := tuple[index].(map[string]any)
			return child
		}
		child, _ := schema["additionalItems"].(map[string]any)
		return child
	}
	child, _ := schema["items"].(map[string]any)
	return child
}
