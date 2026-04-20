package command

import (
	"reflect"
	"strings"
	"testing"
)

// TestValidateSpec_MatchesAcceptInput is the minimum-viable lint: the
// one migrated command (quest_accept) must have every ArgSpec match a
// json field on its input struct, and vice versa. QUEST-45 will
// generalize this to walk every registered command.
func TestValidateSpec_MatchesAcceptInput(t *testing.T) {
	// Mirror the live spec to avoid a circular import on internal/quest.
	args := []ArgSpec{
		{Name: "quest_id", Kind: ArgPositional, Type: ArgString, Required: true, Help: "QUEST-NNN"},
		{Name: "owner", Kind: ArgFlag, Type: ArgString, Help: "claim owner (defaults to 'agent')", CLIOnly: true},
		{Name: "project", Kind: ArgFlag, Type: ArgString, Help: "project override", Short: "p"},
	}
	type input struct {
		QuestID string `json:"quest_id"`
		Owner   string `json:"owner,omitempty"`
		Project string `json:"project,omitempty"`
	}
	if err := validateSpec(args, reflect.TypeFor[input]()); err != nil {
		t.Fatalf("validateSpec: %v", err)
	}
}

func TestValidateSpec_RejectsMissingArgForField(t *testing.T) {
	args := []ArgSpec{
		{Name: "quest_id", Kind: ArgPositional, Type: ArgString, Help: "QUEST-NNN"},
	}
	type input struct {
		QuestID string `json:"quest_id"`
		Owner   string `json:"owner,omitempty"` // no matching ArgSpec
	}
	if err := validateSpec(args, reflect.TypeFor[input]()); err == nil {
		t.Fatal("expected error for unmatched input field")
	}
}

func TestValidateSpec_RejectsEmptyHelp(t *testing.T) {
	args := []ArgSpec{
		{Name: "quest_id", Kind: ArgPositional, Type: ArgString, Help: ""},
	}
	type input struct {
		QuestID string `json:"quest_id"`
	}
	if err := validateSpec(args, reflect.TypeFor[input]()); err == nil {
		t.Fatal("expected error for empty Help")
	}
}

// TestValidateArgFieldKind_CatchesMismatch is the regression for the
// lore_meld panic (2026-04-19): ArgSpec.Type=ArgString declared but
// the struct field was float64 — setField(SetString, float64) panics
// at runtime. validateSpec now rejects that at init time.
func TestValidateArgFieldKind_CatchesMismatch(t *testing.T) {
	args := []ArgSpec{
		{Name: "threshold", Kind: ArgFlag, Type: ArgString, Help: "float value"},
	}
	type input struct {
		Threshold float64 `json:"threshold,omitempty"`
	}
	err := validateSpec(args, reflect.TypeFor[input]())
	if err == nil {
		t.Fatal("expected mismatch error for ArgString→float64")
	}
	if !strings.Contains(err.Error(), "threshold") {
		t.Errorf("error should name the mismatched arg: %v", err)
	}
}
