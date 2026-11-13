package home

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"quasarbridge/internal/llm"
)

func TestFastTrackLearnsConfirmedSwitchOff(t *testing.T) {
	state, calls, plans := "on", 0, 0
	tools := toolFunc(func(_ context.Context, name string, args map[string]any) (jsontext.Value, error) {
		var v any
		switch name {
		case "ha_get_skill_guide":
			v = map[string]any{"success": true}
		case "ha_search":
			v = map[string]any{"entities": []any{map[string]any{"entity_id": "switch.office", "friendly_name": "Розетка", "state": state, "area": "Кабинет"}}, "has_more": false}
		case "ha_get_entity_exposure":
			v = map[string]any{"exposed_to": map[string]any{"conversation": true}}
		case "ha_call_service":
			calls++
			if args["service"] != "turn_off" {
				t.Fatal("wrong action")
			}
			state = "off"
			v = map[string]any{"success": true}
		case "ha_get_state":
			v = map[string]any{"data": map[string]any{"state": state}}
		default:
			t.Fatalf("unexpected %s", name)
		}
		b, _ := json.Marshal(v)
		return b, nil
	})
	c := Controller{Tools: tools, Domains: []string{"switch"}, FastTrack: &FastTrack{}, Planner: plannerFunc(func(context.Context, []llm.Message, bool) (string, error) {
		plans++
		return `{"answer":"ok","actions":[{"domain":"switch","service":"turn_off","entity_id":"switch.office","data":{}}],"learning":{"groups":[["кабинет"],["розетку"],["выключи"]]}}`, nil
	})}
	phrase := "выключи розетку в кабинете сейчас"
	if _, err := c.Handle(t.Context(), phrase); err != nil {
		t.Fatal(err)
	}
	if !c.HasFastTrack("пожалуйста выключи розетку в кабинете") {
		t.Fatal("semantic route missing")
	}
	if c.HasFastTrack("пожалуйста не выключи розетку в кабинете") {
		t.Fatal("negation matched")
	}
	state = "on"
	if _, err := c.Handle(t.Context(), phrase); err != nil {
		t.Fatal(err)
	}
	if plans != 1 || calls != 2 || state != "off" {
		t.Fatalf("planner=%d calls=%d state=%s", plans, calls, state)
	}
}

func TestBinaryActionSemanticDirection(t *testing.T) {
	for _, tc := range []struct {
		domain, service, text string
		want                  bool
	}{
		{"light", "turn_off", "выключи свет в спальне", true},
		{"switch", "turn_on", "включи розетку в спальне", true},
		{"switch", "turn_off", "выключи розетку в спальне", true},
		{"fan", "turn_on", "включи вентилятор в спальне", true},
		{"fan", "turn_off", "выключи вентилятор в спальне", true},
		{"fan", "turn_off", "не выключай вентилятор в спальне", false},
		{"switch", "turn_on", "выключи розетку в спальне", false},
		{"switch", "turn_off", "выключи и включи розетку в спальне", false},
		{"fan", "turn_on", "в спальне жарко помоги", false},
	} {
		a := Action{Domain: tc.domain, Service: tc.service}
		if got := safeSemanticText(tc.text, a); got != tc.want {
			t.Errorf("%s.%s %q: got %v", tc.domain, tc.service, tc.text, got)
		}
	}
}

func TestFastTrackDoesNotMatchDifferentIntent(t *testing.T) {
	var index FastTrack
	index.Learn("В спальне темно", Plan{Actions: []Action{{Domain: "light", Service: "turn_on", EntityID: "light.bedroom"}}})
	if _, ok := index.Lookup("В спальне светло"); ok {
		t.Fatal("different intent matched")
	}
	if _, ok := index.Lookup("В спальне не темно"); ok {
		t.Fatal("negated intent matched")
	}
	if _, ok := index.Lookup("На кухне темно"); ok {
		t.Fatal("different room matched")
	}
	got, ok := index.Lookup("  В СПАЛЬНЕ   темно  ")
	if !ok || len(got.Actions) != 1 || got.Actions[0].EntityID != "light.bedroom" {
		t.Fatalf("unexpected match: %#v, %v", got, ok)
	}
	got.Actions[0].EntityID = "light.other"
	again, _ := index.Lookup("В спальне темно")
	if again.Actions[0].EntityID != "light.bedroom" {
		t.Fatal("stored plan was mutated by caller")
	}
}

func TestFastTrackPersistsAndFailsClosedOnCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fasttrack.json")
	index := FastTrack{Path: path}
	index.Learn("В кабинете темно", Plan{Actions: []Action{{Domain: "light", Service: "turn_on", EntityID: "light.office"}}})
	restarted := FastTrack{Path: path}
	if _, ok := restarted.Lookup("в кабинете темно"); !ok {
		t.Fatal("lost learned action after restart")
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Lookup("в кабинете темно"); ok {
		t.Fatal("corrupt file must fail closed")
	}
}

func TestFastTrackLearnsOnlyConfirmedActionAndRechecksExposure(t *testing.T) {
	f := &homeFixture{state: "off"}
	calls := 0
	c := Controller{Tools: f, Domains: []string{"light"}, FastTrack: &FastTrack{}, Planner: plannerFunc(func(context.Context, []llm.Message, bool) (string, error) {
		calls++
		return `{"answer":"ok","actions":[{"domain":"light","service":"turn_on","entity_id":"light.office","data":{}}]}`, nil
	})}
	phrase := "В кабинете темно, помоги"
	ctx := WithOriginalUtterance(context.Background(), phrase)
	if _, err := c.Handle(ctx, "Включи свет в кабинете"); err != nil {
		t.Fatal(err)
	}
	if !c.HasFastTrack(phrase) || c.HasFastTrack("В кабинете светло") {
		t.Fatal("incorrect learned phrase")
	}
	if _, err := c.Handle(ctx, phrase); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || f.actions != 2 {
		t.Fatalf("planner=%d actions=%d", calls, f.actions)
	}
	f.notExposed = true
	if _, err := c.Handle(ctx, phrase); err == nil {
		t.Fatal("must recheck exposure")
	}
	if f.actions != 2 {
		t.Fatal("called service after exposure revoked")
	}
}

func TestFastTrackDoesNotLearnFailedOrContextualAction(t *testing.T) {
	f := &homeFixture{state: "off", fail: true}
	c := Controller{Tools: f, Domains: []string{"light"}, FastTrack: &FastTrack{}, Planner: plannerFunc(func(context.Context, []llm.Message, bool) (string, error) {
		return `{"answer":"ok","actions":[{"domain":"light","service":"turn_on","entity_id":"light.office","data":{}}]}`, nil
	})}
	phrase := "В кабинете темно"
	if _, err := c.Handle(context.Background(), phrase); err == nil {
		t.Fatal("unconfirmed action")
	}
	if c.HasFastTrack(phrase) {
		t.Fatal("learned unconfirmed action")
	}
	f.fail = false
	if _, err := c.Handle(context.Background(), "Если в кабинете темно"); err != nil {
		t.Fatal(err)
	}
	if c.HasFastTrack("Если в кабинете темно") {
		t.Fatal("learned conditional request")
	}
}

func TestSemanticFastTrackUsesOnlyObservedAnchorsAndRejectsNegation(t *testing.T) {
	f := &homeFixture{state: "off"}
	calls := 0
	c := Controller{Tools: f, Domains: []string{"light"}, FastTrack: &FastTrack{}, Planner: plannerFunc(func(context.Context, []llm.Message, bool) (string, error) {
		calls++
		return `{"answer":"ok","actions":[{"domain":"light","service":"turn_on","entity_id":"light.office","data":{}}],"learning":{"groups":[["кабинет"],["темно","сумрак"],["помоги"]],"exclude":["кино"]}}`, nil
	})}
	if _, err := c.Handle(context.Background(), "В кабинете темно, помоги"); err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"пожалуйста, в кабинете темно, помоги", "в кабинете темно помоги сейчас"} {
		if !c.HasFastTrack(phrase) {
			t.Fatalf("missed safe variant %q", phrase)
		}
	}
	for _, phrase := range []string{"в кабинете не темно, помоги", "если в кабинете темно помоги", "в кабинете сумрак помоги", "в спальне темно помоги", "объясни почему в кабинете темно помоги", "в кабинете темно помоги выключить свет", "в кабинете темно помоги включить кино"} {
		if c.HasFastTrack(phrase) {
			t.Fatalf("unsafe semantic match %q", phrase)
		}
	}
	if _, err := c.Handle(context.Background(), "пожалуйста, в кабинете темно, помоги"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || f.actions != 2 {
		t.Fatalf("planner=%d actions=%d", calls, f.actions)
	}
}

func TestRoomMentionsHandleRussianInflectionAndAmbiguity(t *testing.T) {
	plan := Plan{Actions: []Action{{Domain: "light", Service: "turn_on", EntityID: "light.kitchen"}}}
	known := map[string]Entity{
		"light.kitchen": {ID: "light.kitchen", Area: "Кухня"},
		"light.bedroom": {ID: "light.bedroom", Area: "Спальня"},
	}
	if !explicitArea("на кухне темно помоги", plan, known) {
		t.Fatal("kitchen inflection was not recognized")
	}
	if explicitArea("на кухне и в спальне темно помоги", plan, known) {
		t.Fatal("ambiguous rooms were accepted")
	}
	if !mentionsArea("я в моей комнате", "Моя комната") {
		t.Fatal("multiword area was not recognized")
	}
}

func TestSemanticLearningRejectsDuplicatedGroups(t *testing.T) {
	plan := Plan{Actions: []Action{{Domain: "light", Service: "turn_on", EntityID: "light.office"}}, Learning: &SemanticLearning{Groups: [][]string{{"кабинет"}, {"кабинет"}, {"помоги"}}}}
	known := map[string]Entity{"light.office": {ID: "light.office", Area: "Кабинет"}}
	if validatedLearning("в кабинете темно помоги", plan, known) != nil {
		t.Fatal("duplicated groups accepted")
	}
}
