package command

import (
	"reflect"
	"testing"
)

func TestSynthArgValues_PerType(t *testing.T) {
	args := []ArgSpec{
		{Name: "title", Type: ArgString, Help: "text"},
		{Name: "quest_id", Type: ArgString, Help: "id"},
		{Name: "entry_id", Type: ArgString, Help: "entry id"},
		{Name: "limit", Type: ArgInt, Help: "count"},
		{Name: "all_projects", Type: ArgBool, Help: "flag"},
		{Name: "tags", Type: ArgStringSlice, Help: "slice"},
	}

	got := SynthArgValues(args)

	if v, ok := got["title"].(string); !ok || v != "x" {
		t.Errorf("title: want %q, got %v", "x", got["title"])
	}
	if v, ok := got["quest_id"].(string); !ok || v != "QUEST-1" {
		t.Errorf("quest_id: want %q, got %v", "QUEST-1", got["quest_id"])
	}
	if v, ok := got["entry_id"].(string); !ok || v != "LORE-1" {
		t.Errorf("entry_id: want %q, got %v", "LORE-1", got["entry_id"])
	}
	if v, ok := got["limit"].(int); !ok || v != 1 {
		t.Errorf("limit: want 1, got %v", got["limit"])
	}
	if v, ok := got["all_projects"].(bool); !ok || !v {
		t.Errorf("all_projects: want true, got %v", got["all_projects"])
	}
	if v, ok := got["tags"].([]string); !ok || !reflect.DeepEqual(v, []string{"a"}) {
		t.Errorf("tags: want [\"a\"], got %v", got["tags"])
	}
}

func TestSynthArgValues_SkipsCLIOnly(t *testing.T) {
	args := []ArgSpec{
		{Name: "visible", Type: ArgString, Help: "shown"},
		{Name: "hidden", Type: ArgString, Help: "cli only", CLIOnly: true},
	}
	got := SynthArgValues(args)
	if _, ok := got["hidden"]; ok {
		t.Error("CLIOnly arg should not appear in synth output")
	}
	if _, ok := got["visible"]; !ok {
		t.Error("non-CLIOnly arg should appear in synth output")
	}
}

func TestSynthArgValues_IDSuffixVariants(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"old_id", "LORE-1"},
		{"new_id", "LORE-1"},
		{"from_id", "LORE-1"},
		{"to_id", "LORE-1"},
		{"entry_id", "LORE-1"},
		{"project", "x"},
		{"subject", "x"},
	}
	for _, c := range cases {
		got := synthString(c.name)
		if got != c.want {
			t.Errorf("synthString(%q) = %q; want %q", c.name, got, c.want)
		}
	}
}
