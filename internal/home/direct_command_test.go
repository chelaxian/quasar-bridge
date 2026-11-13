package home

import (
	"context"
	"testing"

	"quasarbridge/internal/llm"
)

func TestDirectHomeCommandIsConservative(t *testing.T) {
	lights := []Entity{{ID: "light.office", Area: "Кабинет", State: "off"}}
	for _, tc := range []struct {
		text     string
		entities []Entity
		want     bool
	}{
		{"включи свет в кабинете", lights, true},
		{"не включи свет в кабинете", lights, false},
		{"включи свет в спальне", lights, false},
		{"если темно включи свет в кабинете", lights, false},
		{"включи свет в кабинете на 20 процентов", lights, false},
		{"включи свет в кабинете", append(append([]Entity{}, lights...), Entity{ID: "light.second", Area: "Кабинет", State: "off"}), false},
		{"включи свет в кабинете", append(append([]Entity{}, lights...), Entity{ID: "light.second", Area: "Кабинет", State: "unavailable"}), false},
		{"включи свет в кабинете", []Entity{{ID: "light.office", Area: "Кабинет", State: "unavailable"}}, false},
	} {
		_, got := directHomeCommand(tc.text, tc.entities)
		if got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestDirectHomeCommandSkipsPlannerAndConfirms(t *testing.T) {
	f := &homeFixture{state: "off"}
	c := Controller{Tools: f, Domains: []string{"light"}, FastTrack: &FastTrack{}, Planner: plannerFunc(func(context.Context, []llm.Message, bool) (string, error) {
		t.Fatal("direct request called planner")
		return "", nil
	})}
	answer, err := c.Handle(t.Context(), "Включи свет в кабинете")
	if err != nil || f.actions != 1 || f.state != "on" || answer == "" {
		t.Fatalf("answer=%q actions=%d state=%s err=%v", answer, f.actions, f.state, err)
	}
	if !c.HasFastTrack("Включи свет в кабинете") {
		t.Fatal("confirmed direct action was not learned")
	}
}

func TestDirectCommandsWithObservedRoomShape(t *testing.T) {
	entities := []Entity{
		{ID: "light.svet_4", Area: "Спальня", State: "off"},
		{ID: "light.svet_1", Area: "Кухня", State: "off"},
		{ID: "light.svet_2_2", Area: "Кухня", State: "off"},
		{ID: "light.blizhnii_svet", Area: "Коридор", State: "off"},
		{ID: "light.dalnii_svet", Area: "Коридор", State: "off"},
	}
	plan, ok := directHomeCommand("включи свет в спальне", entities)
	if !ok || plan.Actions[0].EntityID != "light.svet_4" {
		t.Fatalf("bedroom=%+v %v", plan, ok)
	}
	for _, phrase := range []string{"включи свет на кухне", "выключи свет в коридоре"} {
		if _, ok := directHomeCommand(phrase, entities); ok {
			t.Fatalf("ambiguous command matched: %s", phrase)
		}
	}
}
