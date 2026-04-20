package mcp

import (
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// flexIntSchema builds a JSON Schema for the input type In and relaxes
// every property whose Go type is flexInt64 so the schema accepts
// BOTH the JSON integer form (42) AND the JSON string form ("42").
//
// This is QUEST-14's fix: the upstream SDK validates arguments against
// the generated schema BEFORE unmarshal, so a string-encoded id like
// "542" is rejected with "got string, want integer" even though
// flexInt64's UnmarshalJSON would happily accept it. By post-processing
// the generated schema to use JSON Schema's `type: ["integer","string"]`
// (with a digit-only pattern on the string branch) we let both forms
// through validation and let flexInt64 normalize them on unmarshal.
//
// Caller usage (register.go):
//
//	tool.InputSchema = flexIntSchema(reflect.TypeOf(loreStudyInput{}))
//	sdkmcp.AddTool(s, tool, handleLoreStudy)
//
// Panics on reflection / schema errors — construction-time programmer
// mistakes, not runtime inputs.
func flexIntSchema(inputType reflect.Type) *jsonschema.Schema {
	schema, err := jsonschema.ForType(inputType, &jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Errorf("flexIntSchema: ForType(%v): %w", inputType, err))
	}
	// Find every Go field of type flexInt64 and relax the corresponding
	// JSON Schema property. We walk the struct fields (not the schema
	// properties map) because only reflection knows the field Go types.
	relaxed := collectFlexIntFields(inputType)
	for jsonName := range relaxed {
		prop, ok := schema.Properties[jsonName]
		if !ok {
			// Struct has a flexInt64 field but no corresponding schema
			// property — shouldn't happen for exported json-tagged
			// fields. Skip rather than panic to stay forgiving.
			continue
		}
		// Replace Type: "integer" with Types: ["integer","string"] and
		// attach a digit-only pattern so string-form values still round-
		// trip through flexInt64's ParseInt.
		prop.Type = ""
		prop.Types = []string{"integer", "string"}
		// Pattern applies only to the string branch in practice (the
		// validator ignores pattern for non-string values). Matches
		// optional leading minus sign + digits.
		prop.Pattern = `^-?[0-9]+$`
	}
	return schema
}

// collectFlexIntFields walks inputType's exported fields and returns
// the set of JSON property names whose Go type is flexInt64 (or a
// pointer to one). Only top-level fields are inspected; nested
// structs are left alone because none of the current tool inputs use
// them for IDs.
func collectFlexIntFields(t reflect.Type) map[string]struct{} {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := map[string]struct{}{}
	if t.Kind() != reflect.Struct {
		return out
	}
	flexType := reflect.TypeOf(flexInt64(0))
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

// jsonFieldName returns the JSON name used in the wire form for a
// struct field, honoring the first token of the `json:` tag. Returns
// the empty string when the field is explicitly ignored (`json:"-"`)
// and the Go field name when no tag is present.
func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	if tag == "-" {
		return "-"
	}
	// Strip ",omitempty" / ",omitzero" / etc.
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return tag[:i]
		}
	}
	return tag
}
