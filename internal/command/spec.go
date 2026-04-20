package command

// ArgKind distinguishes positional CLI args from named flags. MCP
// doesn't have the distinction — every arg is a named JSON property —
// but cobra needs to know which args land in cmd.Args vs cmd.Flags.
type ArgKind int

const (
	// ArgFlag is a named flag: --owner VALUE in CLI, {"owner": "..."}
	// in MCP. Default kind for every ArgSpec unless explicitly set.
	ArgFlag ArgKind = iota
	// ArgPositional is a positional CLI argument. In MCP it still
	// appears as a named JSON property — positionality is CLI-only.
	ArgPositional
)

// ArgType is the primitive type of an argument. Aligned with cobra's
// flag types and JSON schema primitive types.
type ArgType int

const (
	ArgString ArgType = iota
	ArgInt
	ArgBool
	ArgStringSlice
)

// ArgSpec describes one argument to a Command in a surface-neutral way.
// The same spec drives cobra flag registration AND MCP JSON schema
// generation. The input-struct field with a matching json tag receives
// the parsed value via reflection.
type ArgSpec struct {
	// Name is the JSON field name in MCP and the default long-flag name
	// in CLI (dashed form). Must match the corresponding input-struct
	// field's `json:"..."` tag exactly — a conformance test enforces
	// this.
	Name string
	// CLIFlagName overrides the CLI long-flag name. Used when MCP wants
	// a structural field name (`to_id`) but CLI wants a semantic verb
	// (`informs`). Empty means "derive from Name".
	CLIFlagName string
	// Short is the single-letter cobra shorthand (e.g. "a" → -a). Empty
	// for no shorthand. Ignored by MCP.
	Short string
	Kind  ArgKind
	Type  ArgType
	// Required marks the arg as mandatory. CLI adapter enforces via
	// cobra.ExactArgs / flag validation; MCP adapter adds to the schema's
	// required array.
	Required bool
	// Default is the zero-value override. Must match Type. Nil means
	// "use Go zero value".
	Default any
	// Help is the single-source description shown in both cobra's flag
	// usage and MCP's jsonschema description. Must be non-empty — the
	// lint test asserts this.
	Help string
	// Repeatable marks a StringSlice as repeatable (cobra StringArrayVar)
	// rather than comma-split (cobra StringSliceVar). Ignored for other
	// types and in MCP (which always accepts an array).
	Repeatable bool
	// CLIOnly suppresses the arg on the MCP surface. Example: --no-emoji
	// has no meaning to an MCP caller.
	CLIOnly bool
	// MCPOnly suppresses the arg on the CLI surface.
	MCPOnly bool
	// Variadic marks a positional arg that absorbs all remaining CLI
	// args (joined by single spaces). Ignored by the MCP surface (always
	// a single JSON string). Only the final positional may be variadic,
	// and only ArgString is supported.
	Variadic bool
}
