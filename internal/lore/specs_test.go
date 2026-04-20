package lore_test

import (
	"reflect"
	"testing"

	"github.com/mathomhaus/guild/internal/command"
	"github.com/mathomhaus/guild/internal/lore"
)

// TestAllCommandSpecs_ArgFieldKindAlignment is the lore-side sibling of
// the quest-package test. Same rationale: catch ArgSpec.Type ↔ field
// kind mismatches at init time instead of runtime setField panics.
func TestAllCommandSpecs_ArgFieldKindAlignment(t *testing.T) {
	cases := []struct {
		name      string
		args      []command.ArgSpec
		inputType reflect.Type
	}{
		{"lore.OathCommand", lore.OathCommand.Args, reflect.TypeFor[lore.OathInput]()},
		{"lore.DossierCommand", lore.DossierCommand.Args, reflect.TypeFor[lore.DossierInput]()},
		{"lore.SealCommand", lore.SealCommand.Args, reflect.TypeFor[lore.SealInput]()},
		{"lore.LinkCommand", lore.LinkCommand.Args, reflect.TypeFor[lore.LinkInput]()},
		{"lore.ReforgeCommand", lore.ReforgeCommand.Args, reflect.TypeFor[lore.ReforgeInput]()},
		{"lore.InscribeCommand", lore.InscribeCommand.Args, reflect.TypeFor[lore.InscribeInput]()},
		{"lore.UpdateCommand", lore.UpdateCommand.Args, reflect.TypeFor[lore.UpdateInput]()},
		{"lore.CatalogCommand", lore.CatalogCommand.Args, reflect.TypeFor[lore.CatalogInput]()},
		{"lore.EchoesCommand", lore.EchoesCommand.Args, reflect.TypeFor[lore.EchoesInput]()},
		{"lore.WhispersCommand", lore.WhispersCommand.Args, reflect.TypeFor[lore.WhispersInput]()},
		{"lore.ListCommand", lore.ListCommand.Args, reflect.TypeFor[lore.ListInput]()},
		{"lore.InquestCommand", lore.InquestCommand.Args, reflect.TypeFor[lore.InquestInput]()},
		{"lore.MeldCommand", lore.MeldCommand.Args, reflect.TypeFor[lore.MeldInput]()},
		{"lore.CommuneCommand", lore.CommuneCommand.Args, reflect.TypeFor[lore.CommuneInput]()},
		{"lore.AppraiseCommand", lore.AppraiseCommand.Args, reflect.TypeFor[lore.AppraiseInput]()},
		{"lore.StudyCommand", lore.StudyCommand.Args, reflect.TypeFor[lore.StudyInput]()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := command.ValidateSpec(tc.args, tc.inputType); err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
		})
	}
}
