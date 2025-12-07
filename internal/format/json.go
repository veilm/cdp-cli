package format

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// JSON returns a string representation of the provided value.
func JSON(value interface{}, pretty bool, maxDepth int) (string, error) {
	pruned := prune(value, maxDepth)
	data, err := json.Marshal(pruned)
	if err != nil {
		return "", err
	}
	if !pretty {
		return string(data), nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func prune(value interface{}, depth int) interface{} {
	if depth == 0 {
		return "..."
	}
	switch v := value.(type) {
	case map[string]interface{}:
		nextDepth := decrement(depth)
		clone := make(map[string]interface{}, len(v))
		for key, val := range v {
			clone[key] = prune(val, nextDepth)
		}
		return clone
	case []interface{}:
		nextDepth := decrement(depth)
		clone := make([]interface{}, len(v))
		for i, val := range v {
			clone[i] = prune(val, nextDepth)
		}
		return clone
	case json.RawMessage:
		var decoded interface{}
		if err := json.Unmarshal(v, &decoded); err == nil {
			return prune(decoded, depth)
		}
		return string(v)
	default:
		return value
	}
}

func decrement(depth int) int {
	if depth < 0 {
		return depth
	}
	return depth - 1
}

// MustJSON is a helper for debugging (panics on error).
func MustJSON(value interface{}, pretty bool, maxDepth int) string {
	out, err := JSON(value, pretty, maxDepth)
	if err != nil {
		panic(fmt.Sprintf("json encode failed: %v", err))
	}
	return out
}
