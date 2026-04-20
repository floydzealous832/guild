package command

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// parseFlexInt accepts "LORE-42", "lore-42", "ENTRY-42", "entry-42",
// "QUEST-7", "quest-7", or a bare decimal integer. Returns the numeric
// id or an error. Used by the CLI path to parse positional ID args into
// FlexInt64 fields.
func parseFlexInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"LORE-", "lore-", "ENTRY-", "entry-", "QUEST-", "quest-"} {
		s = strings.TrimPrefix(s, prefix)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q: %w", s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("id must be positive, got %d", n)
	}
	return n, nil
}

// jsonFieldName returns the JSON name used on the wire for a struct
// field, honoring the first token of the `json:` tag. Returns the empty
// string when the field is explicitly ignored (`json:"-"`) and the Go
// field name when no tag is present.
//
// Duplicated from internal/mcp/flex_schema.go to keep internal/command
// free of MCP package imports.
func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	if tag == "-" {
		return "-"
	}
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return tag[:i]
		}
	}
	return tag
}

// fieldByJSONName finds the struct field on v (must be an addressable
// struct) whose json tag matches name. Returns the reflect.Value and
// true, or the zero Value and false.
func fieldByJSONName(v reflect.Value, name string) (reflect.Value, bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if jsonFieldName(f) == name {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// setField assigns val to fv, coercing from the ArgSpec's declared Type.
// Returns an error on type mismatch so the adapter can surface a tidy
// usage error rather than panic.
func setField(fv reflect.Value, argType ArgType, val any) error {
	if !fv.CanSet() {
		return fmt.Errorf("field not settable")
	}
	// Special-case: FlexInt64 target accepts a string input. On the
	// CLI path, positional args like "LORE-42" or legacy "ENTRY-42"
	// come in as a string; we parse and coerce (stripping the LORE- /
	// ENTRY- / entry- prefix) and assign to the FlexInt64 field. On
	// the MCP path the SDK already unmarshals into FlexInt64 before
	// setField runs, so this branch is CLI-only in practice.
	if fv.Type() == reflect.TypeFor[FlexInt64]() && argType == ArgString {
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("FlexInt64: expected string, got %T", val)
		}
		n, perr := parseFlexInt(s)
		if perr != nil {
			return perr
		}
		fv.Set(reflect.ValueOf(FlexInt64(n)))
		return nil
	}
	switch argType {
	case ArgString:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		fv.SetString(s)
	case ArgInt:
		switch n := val.(type) {
		case int:
			fv.SetInt(int64(n))
		case int64:
			fv.SetInt(n)
		case float64:
			fv.SetInt(int64(n))
		default:
			return fmt.Errorf("expected int, got %T", val)
		}
	case ArgBool:
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T", val)
		}
		fv.SetBool(b)
	case ArgStringSlice:
		ss, ok := val.([]string)
		if !ok {
			// Tolerate []any from JSON unmarshal.
			if anys, ok2 := val.([]any); ok2 {
				ss = make([]string, 0, len(anys))
				for _, a := range anys {
					s, _ := a.(string)
					ss = append(ss, s)
				}
			} else {
				return fmt.Errorf("expected []string, got %T", val)
			}
		}
		fv.Set(reflect.ValueOf(ss))
	default:
		return fmt.Errorf("unsupported ArgType %d", argType)
	}
	return nil
}

// validateArgFieldKind enforces ArgSpec.Type ↔ Go field type alignment.
// ArgString → string | FlexInt64 (the latter for positional-ID coercion).
// ArgInt    → int / int32 / int64.
// ArgBool   → bool.
// ArgStringSlice → []string.
// Any other combo would panic at runtime in setField; we want that to
// fail at init-test time instead.
func validateArgFieldKind(name string, fieldType reflect.Type, argType ArgType) error {
	ft := fieldType
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	flex := reflect.TypeFor[FlexInt64]()
	switch argType {
	case ArgString:
		if ft.Kind() == reflect.String || ft == flex {
			return nil
		}
		return fmt.Errorf("arg %q: ArgString declared but field is %s", name, ft.Kind())
	case ArgInt:
		switch ft.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return nil
		}
		return fmt.Errorf("arg %q: ArgInt declared but field is %s", name, ft.Kind())
	case ArgBool:
		if ft.Kind() == reflect.Bool {
			return nil
		}
		return fmt.Errorf("arg %q: ArgBool declared but field is %s", name, ft.Kind())
	case ArgStringSlice:
		if ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.String {
			return nil
		}
		return fmt.Errorf("arg %q: ArgStringSlice declared but field is %s", name, ft.String())
	}
	return fmt.Errorf("arg %q: unknown ArgType %d", name, argType)
}

// ValidateSpec is the exported wrapper around validateSpec — lets
// per-domain test files (internal/quest, internal/lore) assert their
// own Command specs at test-init time. Fails on any of:
//   - missing ArgSpec for an exported json field on the input struct
//   - ArgSpec naming a field that doesn't exist
//   - empty Name or Help
//   - ArgSpec.Type doesn't match the Go field kind (regression gate
//     for the lore_meld panic; see 2026-04-19).
func ValidateSpec(args []ArgSpec, inputType reflect.Type) error {
	return validateSpec(args, inputType)
}

// validateSpec asserts that every Arg's Name matches a json field on
// the input struct, and that every exported json field on the input
// struct has a matching Arg. Run from a test at build time.
func validateSpec(args []ArgSpec, inputType reflect.Type) error {
	for inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	if inputType.Kind() != reflect.Struct {
		return fmt.Errorf("input type must be struct, got %s", inputType.Kind())
	}
	argNames := map[string]bool{}
	for _, a := range args {
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("arg has empty Name")
		}
		if strings.TrimSpace(a.Help) == "" {
			return fmt.Errorf("arg %q has empty Help", a.Name)
		}
		argNames[a.Name] = true
	}
	// Cross-reference field Go types with ArgSpec.Type so we catch
	// declaration mismatches at test-init time instead of panicking at
	// runtime inside setField. The rule: every ArgSpec must match its
	// struct field's kind (modulo FlexInt64 for positional ID coercion).
	argByName := map[string]ArgSpec{}
	for _, a := range args {
		argByName[a.Name] = a
	}
	fieldNames := map[string]bool{}
	for i := 0; i < inputType.NumField(); i++ {
		f := inputType.Field(i)
		if !f.IsExported() {
			continue
		}
		name := jsonFieldName(f)
		if name == "" || name == "-" {
			continue
		}
		fieldNames[name] = true
		a, ok := argByName[name]
		if !ok {
			return fmt.Errorf("input field %q has no matching ArgSpec", name)
		}
		if err := validateArgFieldKind(name, f.Type, a.Type); err != nil {
			return err
		}
	}
	for name := range argNames {
		if !fieldNames[name] {
			return fmt.Errorf("ArgSpec %q has no matching input field", name)
		}
	}
	return nil
}
