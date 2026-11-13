package home

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

type utteranceKey struct{}

// SemanticLearning is a model-suggested conjunction of observed fragments.
// It is deliberately not an embedding similarity score or a general LLM.
type SemanticLearning struct {
	Groups  [][]string `json:"groups"`
	Exclude []string   `json:"exclude,omitempty"`
}

// WithOriginalUtterance preserves the user's words when an assistant tool
// paraphrases its request to the home planner.
func WithOriginalUtterance(ctx context.Context, text string) context.Context {
	return context.WithValue(ctx, utteranceKey{}, text)
}
func originalUtterance(ctx context.Context, fallback string) string {
	if text, ok := ctx.Value(utteranceKey{}).(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
}

// FastTrack stores confirmed plans with exact-phrase lookup and, when
// available, conservative model-suggested anchor groups for one light.
// All plans still pass validation, exposure and confirmation checks.
type FastTrack struct {
	mu sync.Mutex
	// Path is outside the bridge installation so updates preserve learning.
	// Empty enables the in-memory index (useful for tests).
	Path  string
	plans map[string]Plan
	info  os.FileInfo
}

func fastTrackKey(text string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// clonePlan prevents callers from mutating a stored action after learning.
func clonePlan(plan Plan) Plan {
	copy := Plan{Answer: plan.Answer, Actions: make([]Action, len(plan.Actions))}
	if plan.Learning != nil {
		copy.Learning = &SemanticLearning{Groups: make([][]string, len(plan.Learning.Groups)), Exclude: append([]string(nil), plan.Learning.Exclude...)}
		for i, group := range plan.Learning.Groups {
			copy.Learning.Groups[i] = append([]string(nil), group...)
		}
	}
	for i, a := range plan.Actions {
		copy.Actions[i] = a
		if a.Data != nil {
			copy.Actions[i].Data = make(map[string]any, len(a.Data))
			for k, v := range a.Data {
				copy.Actions[i].Data[k] = v
			}
		}
	}
	return copy
}

// Learn records only an action plan the caller has already verified.
func (f *FastTrack) Learn(text string, plan Plan) {
	key := fastTrackKey(text)
	if key == "" || len(plan.Actions) == 0 {
		return
	}
	// Parameterized actions can contain nested mutable values and require
	// additional semantic checks. Do not learn them in this initial index.
	for _, a := range plan.Actions {
		if len(a.Data) != 0 {
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.reload(); err != nil {
		slog.Warn("fast_track_read_failed", "error", err)
		return
	}
	if f.plans == nil {
		f.plans = make(map[string]Plan)
	}
	if len(f.plans) >= 1000 {
		return
	}
	f.plans[key] = clonePlan(plan)
	if err := f.save(); err != nil {
		f.info = nil
		slog.Warn("fast_track_write_failed", "error", err)
	}
}

func (f *FastTrack) Lookup(text string) (Plan, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fastTrackKey(text)
	disabledSet := map[string]bool{}
	if f.Path != "" {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(f.Path), "fasttrack.disabled.json"))
		if err != nil && !os.IsNotExist(err) {
			return Plan{}, false
		}
		if err == nil {
			var disabled []string
			if json.Unmarshal(b, &disabled) != nil {
				return Plan{}, false
			}
			for _, value := range disabled {
				disabledSet[value] = true
			}
		}
	}
	if disabledSet[key] {
		return Plan{}, false
	}
	if err := f.reload(); err != nil {
		return Plan{}, false
	}
	plan, ok := f.plans[key]
	if ok {
		return clonePlan(plan), true
	}
	var found Plan
	matched := false
	overrides, validOverrides := f.overrides()
	if !validOverrides {
		return Plan{}, false
	}
	for phrase, candidate := range f.plans {
		if override, ok := overrides[phrase]; ok {
			candidate.Learning = override
		}
		if phrase == key || candidate.Learning == nil || disabledSet[phrase] {
			continue
		}
		if validRule(candidate) && safeSemanticText(key, candidate.Actions[0]) && semanticMatch(key, candidate.Learning) {
			// Multiple matching rules are ambiguous even if they currently point
			// to the same device; fall back to the normal assistant path.
			if matched {
				return Plan{}, false
			}
			found, matched = candidate, true
		}
	}
	if !matched {
		return Plan{}, false
	}
	return clonePlan(found), true
}

func (f *FastTrack) overrides() (map[string]*SemanticLearning, bool) {
	if f.Path == "" {
		return nil, true
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(f.Path), "fasttrack.overrides.json"))
	if os.IsNotExist(err) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	var out map[string]*SemanticLearning
	if len(b) > 1<<20 || json.Unmarshal(b, &out) != nil || out == nil {
		return nil, false
	}
	return out, true
}

func validRule(plan Plan) bool {
	if len(plan.Actions) != 1 || !fastBinaryAction(plan.Actions[0]) || plan.Learning == nil {
		return false
	}
	if len(plan.Learning.Groups) < 3 || len(plan.Learning.Groups) > 5 || len(plan.Learning.Exclude) > 20 {
		return false
	}
	for i, group := range plan.Learning.Groups {
		if len(group) == 0 || len(group) > 8 {
			return false
		}
		for _, term := range group {
			if len([]rune(term)) < 3 || len([]rune(term)) > 50 || term != fastTrackKey(term) {
				return false
			}
			for _, earlier := range plan.Learning.Groups[:i] {
				for _, other := range earlier {
					if strings.Contains(term, other) || strings.Contains(other, term) {
						return false
					}
				}
			}
		}
	}
	for _, term := range plan.Learning.Exclude {
		if len([]rune(term)) < 2 || len([]rune(term)) > 50 || term != fastTrackKey(term) {
			return false
		}
	}
	return true
}

func fastBinaryAction(a Action) bool {
	return (a.Domain == "light" || a.Domain == "switch" || a.Domain == "fan") && (a.Service == "turn_on" || a.Service == "turn_off") && len(a.Data) == 0
}

func safeSemanticText(text string, action Action) bool {
	s := strings.ToLower(text)
	off := strings.Contains(s, "выключ") || strings.Contains(s, "отключ") || strings.Contains(s, "погаси")
	on := strings.Contains(s, "зажги") || strings.Contains(s, "запусти")
	for _, word := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if strings.HasPrefix(word, "включ") {
			on = true
		}
	}
	if action.Service == "turn_on" && off {
		return false
	}
	if action.Service == "turn_off" && (!off || on) {
		return false
	}
	if action.Service == "turn_on" && action.Domain != "light" && !on {
		return false
	}
	// Negation in the commonly used conditions "не вижу/не видно" is not
	// a prohibition; other negations, hypotheticals and questions fall back.
	s = strings.ReplaceAll(strings.ReplaceAll(s, "не вижу", "вижу"), "не видно", "видно")
	for _, word := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if word == "не" || word == "ли" || word == "как" {
			return false
		}
	}
	for _, r := range s {
		if unicode.IsDigit(r) {
			return false
		}
	}
	for _, term := range []string{"нельзя", "запрет", "если", "когда", "завтра", "потом", "через ", "например", "представь", "почему", "зачем", "расскажи", "объясни", "сколько", "ярк", "процент", "цвет", "красн", "син", "зелён", "зелен", "жёлт", "желт", "тепл", "холодн", "тускл", "слабее", "сильнее"} {
		if strings.Contains(s, term) {
			return false
		}
	}
	return true
}

func semanticMatch(text string, rule *SemanticLearning) bool {
	if rule == nil || len(rule.Groups) < 3 {
		return false
	}
	for _, term := range rule.Exclude {
		if term != "" && strings.Contains(text, term) {
			return false
		}
	}
	for _, group := range rule.Groups {
		ok := false
		for _, term := range group {
			if term != "" && strings.Contains(text, term) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func validatedLearning(text string, plan Plan, known map[string]Entity) *SemanticLearning {
	// The initial automatic semantic executor is limited to turning on one
	// light. Other plans still learn exact confirmed utterances.
	if plan.Learning == nil || len(plan.Actions) != 1 || !fastBinaryAction(plan.Actions[0]) {
		return nil
	}
	text = fastTrackKey(text)
	if !safeSemanticText(text, plan.Actions[0]) || len(plan.Learning.Groups) < 3 || len(plan.Learning.Groups) > 5 || len(plan.Learning.Exclude) > 20 {
		return nil
	}
	area := areaStems(known[plan.Actions[0].EntityID].Area)
	if len(area) == 0 {
		return nil
	}
	hasArea := false
	out := &SemanticLearning{}
	for _, group := range plan.Learning.Groups {
		if len(group) == 0 || len(group) > 8 {
			return nil
		}
		var terms []string
		for _, term := range group {
			term = fastTrackKey(term)
			if len([]rune(term)) < 3 || len([]rune(term)) > 50 || !strings.Contains(text, term) {
				continue
			}
			for _, stem := range area {
				if strings.Contains(stem, term) || strings.Contains(term, stem) {
					hasArea = true
				}
			}
			terms = append(terms, term)
		}
		if len(terms) == 0 {
			return nil
		}
		out.Groups = append(out.Groups, terms)
	}
	if !hasArea {
		return nil
	}
	for _, term := range plan.Learning.Exclude {
		term = fastTrackKey(term)
		if len([]rune(term)) < 2 || len([]rune(term)) > 50 {
			return nil
		}
		out.Exclude = append(out.Exclude, term)
	}
	check := plan
	check.Learning = out
	if !validRule(check) {
		return nil
	}
	return out
}

func (f *FastTrack) reload() error {
	if f.Path == "" {
		return nil
	}
	info, err := os.Stat(f.Path)
	if os.IsNotExist(err) {
		f.plans = nil
		f.info = nil
		return nil
	}
	if err != nil {
		return err
	}
	if f.info != nil && os.SameFile(f.info, info) && f.info.Size() == info.Size() && f.info.ModTime().Equal(info.ModTime()) {
		return nil
	}
	b, err := os.ReadFile(f.Path)
	if os.IsNotExist(err) {
		f.plans = nil
		f.info = nil
		return nil
	}
	if err != nil {
		return err
	}
	if len(b) > 2<<20 {
		return os.ErrInvalid
	}
	var entries map[string]Plan
	if err := json.Unmarshal(b, &entries); err != nil {
		return err
	}
	if entries == nil {
		return os.ErrInvalid
	}
	f.plans = entries
	f.info = info
	return nil
}

func (f *FastTrack) save() error {
	if f.Path == "" {
		return nil
	}
	b, err := json.Marshal(f.plans)
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".fasttrack-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(b)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(file.Name(), f.Path); err != nil {
		return err
	}
	f.info = nil
	return nil
}
