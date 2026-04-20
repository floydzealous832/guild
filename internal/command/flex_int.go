package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
)

// FlexInt64 is a JSON-flexible int64 that accepts both numeric and
// string-encoded integer forms in tool arguments.
//
// Background (QUEST-14 in internal/mcp originally): some MCP clients
// serialize numeric tool-argument values as strings. The SDK runs
// strict JSON Schema validation before unmarshal, so "542" against an
// int64 field gets rejected. FlexInt64 solves that in two halves:
//
//  1. Custom UnmarshalJSON accepts both `42` and `"42"`.
//  2. buildMCPSchema detects FlexInt64 fields and rewrites the
//     generated schema's property type to `["integer","string"]` with
//     a digit-only pattern so the SDK's pre-unmarshal validation lets
//     both forms through.
//
// Use FlexInt64 only for input-parameter fields; internal storage
// stays Go int64.
type FlexInt64 int64

func (f *FlexInt64) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = 0
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return fmt.Errorf("FlexInt64: unmarshal string: %w", err)
		}
		if s == "" {
			*f = 0
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("FlexInt64: parse %q as int: %w", s, err)
		}
		*f = FlexInt64(n)
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("FlexInt64: decode number: %w", err)
	}
	num, ok := raw.(json.Number)
	if !ok {
		return fmt.Errorf("FlexInt64: expected number, got %T", raw)
	}
	n, err := num.Int64()
	if err != nil {
		return fmt.Errorf("FlexInt64: non-integer number %q: %w", num.String(), err)
	}
	*f = FlexInt64(n)
	return nil
}

func (f FlexInt64) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(f), 10)), nil
}

// Int64 returns the value as native int64.
func (f FlexInt64) Int64() int64 { return int64(f) }

// relaxFlexIntProperties post-processes a generated jsonschema.Schema
// so every property whose Go type is FlexInt64 accepts both the JSON
// integer form and the string form.
func relaxFlexIntProperties(schema *jsonschema.Schema, inputType reflect.Type) {
	if schema == nil {
		return
	}
	flexFields := collectFlexIntFields(inputType)
	for name := range flexFields {
		prop, ok := schema.Properties[name]
		if !ok {
			continue
		}
		prop.Type = ""
		prop.Types = []string{"integer", "string"}
		prop.Pattern = `^-?[0-9]+$`
	}
}

func collectFlexIntFields(t reflect.Type) map[string]struct{} {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := map[string]struct{}{}
	if t.Kind() != reflect.Struct {
		return out
	}
	flexType := reflect.TypeFor[FlexInt64]()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft != flexType {
			continue
		}
		name := jsonFieldName(f)
		if name == "" || name == "-" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}
