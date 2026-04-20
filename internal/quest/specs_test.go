package quest_test

import (
	"reflect"
	"testing"

	"github.com/mathomhaus/guild/internal/command"
	"github.com/mathomhaus/guild/internal/quest"
)

// TestAllCommandSpecs_ArgFieldKindAlignment sweeps every migrated
// Command spec in internal/quest and asserts ArgSpec.Type matches the
// Go field type. Catches the class of bug that caused the lore_meld
// runtime panic (ArgString declared against a float64 field). Adding
// a new spec with a type mismatch fails this test immediately.
func TestAllCommandSpecs_ArgFieldKindAlignment(t *testing.T) {
	cases := []struct {
		name      string
		args      []command.ArgSpec
		inputType reflect.Type
	}{
		{"quest.AcceptCommand", quest.AcceptCommand.Args, reflect.TypeFor[quest.AcceptInput]()},
		{"quest.ClearCommand", quest.ClearCommand.Args, reflect.TypeFor[quest.ClearInput]()},
		{"quest.ForfeitCommand", quest.ForfeitCommand.Args, reflect.TypeFor[quest.ForfeitInput]()},
		{"quest.JournalCommand", quest.JournalCommand.Args, reflect.TypeFor[quest.JournalInput]()},
		{"quest.BriefCommand", quest.BriefCommand.Args, reflect.TypeFor[quest.BriefInput]()},
		{"quest.ActiveCommand", quest.ActiveCommand.Args, reflect.TypeFor[quest.ActiveInput]()},
		{"quest.SummonCommand", quest.SummonCommand.Args, reflect.TypeFor[quest.SummonInput]()},
		{"quest.OrdersCommand", quest.OrdersCommand.Args, reflect.TypeFor[quest.OrdersInput]()},
		{"quest.CampfireCommand", quest.CampfireCommand.Args, reflect.TypeFor[quest.CampfireInput]()},
		{"quest.EpicCommand", quest.EpicCommand.Args, reflect.TypeFor[quest.EpicInput]()},
		{"quest.PostCommand", quest.PostCommand.Args, reflect.TypeFor[quest.PostInput]()},
		{"quest.UpdateCommand", quest.UpdateCommand.Args, reflect.TypeFor[quest.UpdateInput]()},
		{"quest.ScrollCommand", quest.ScrollCommand.Args, reflect.TypeFor[quest.ScrollInput]()},
		{"quest.ListCommand", quest.ListCommand.Args, reflect.TypeFor[quest.ListInput]()},
		{"quest.GuildCommand", quest.GuildCommand.Args, reflect.TypeFor[quest.GuildInput]()},
		{"quest.PulseCommand", quest.PulseCommand.Args, reflect.TypeFor[quest.PulseInput]()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := command.ValidateSpec(tc.args, tc.inputType); err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
		})
	}
}
