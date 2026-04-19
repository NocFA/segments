package cli

import (
	"encoding/json"
	"testing"
)

func TestCoerceInt(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		def  int
		want int
	}{
		{"float64", float64(2), -1, 2},
		{"int", 3, -1, 3},
		{"int64", int64(4), -1, 4},
		{"json.Number int", json.Number("5"), -1, 5},
		{"string digits", "2", -1, 2},
		{"string padded", "  3 ", -1, 3},
		{"string negative", "-1", 99, -1},
		{"string non-numeric", "hi", 7, 7},
		{"nil", nil, 9, 9},
		{"bool", true, 11, 11},
		{"float with decimal", 2.7, -1, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := coerceInt(c.in, c.def)
			if got != c.want {
				t.Fatalf("coerceInt(%v, %d) = %d, want %d", c.in, c.def, got, c.want)
			}
		})
	}
}

func TestCallToolCreateTaskPriorityAsString(t *testing.T) {
	// Guard: the MCP schema says priority is a "number", but real clients
	// occasionally send it as a string at the top level. callTool must
	// accept both shapes so the priority field is not silently dropped.
	args := map[string]interface{}{
		"priority": "2",
	}
	got := coerceInt(args["priority"], 0)
	if got != 2 {
		t.Fatalf("string priority dropped: got %d, want 2", got)
	}
	args["priority"] = float64(3)
	if got := coerceInt(args["priority"], 0); got != 3 {
		t.Fatalf("number priority dropped: got %d, want 3", got)
	}
}
