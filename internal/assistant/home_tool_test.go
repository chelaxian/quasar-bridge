package assistant

import (
	"context"
	"strings"
	"testing"

	"quasarbridge/internal/llm"
)

type homeControllerFunc func(context.Context, string) (string, error)

func (f homeControllerFunc) Handle(ctx context.Context, request string) (string, error) {
	return f(ctx, request)
}

func TestHomeToolIsExposedOnlyWhenConfigured(t *testing.T) {
	tools := &Tools{Config: DefaultConfig(), Home: homeControllerFunc(func(context.Context, string) (string, error) {
		t.Fatal("definition test must not call home")
		return "", nil
	})}
	if !hasTool(tools.Definitions(), "control_home") {
		t.Fatal("control_home definition missing")
	}

	tools.Config.HomeControl = false
	if hasTool(tools.Definitions(), "control_home") {
		t.Fatal("disabled control_home definition exposed")
	}
	if _, err := tools.Call(t.Context(), "control_home", `{"request":"включи свет"}`); err == nil {
		t.Fatal("disabled control_home executed")
	}
}

func TestHomeToolValidatesAndDelegates(t *testing.T) {
	got := ""
	tools := &Tools{
		Config: DefaultConfig(),
		Home: homeControllerFunc(func(_ context.Context, request string) (string, error) {
			got = request
			return "Готово: Свет в спальне.", nil
		}),
	}
	answer, err := tools.Call(t.Context(), "control_home", `{"request":"в спальне темно и ничего не видно"}`)
	if err != nil || answer != "Готово: Свет в спальне." || got != "в спальне темно и ничего не видно" {
		t.Fatalf("answer=%v request=%q error=%v", answer, got, err)
	}
	for _, args := range []string{`{}`, `{"request":"   "}`, `{"request":""}`, `{"request":"x","extra":1}`} {
		if _, err := tools.Call(t.Context(), "control_home", args); err == nil {
			t.Fatalf("invalid arguments accepted: %s", args)
		}
	}
}

func TestAgentCanCallHomeToolForImplicitLightingRequest(t *testing.T) {
	round := 0
	tools := &Tools{
		Config: DefaultConfig(),
		Home: homeControllerFunc(func(_ context.Context, request string) (string, error) {
			if request != "я нахожусь в спальне и ничего не вижу, помоги мне" {
				t.Fatalf("unexpected home request: %q", request)
			}
			return "Готово: Свет.", nil
		}),
	}
	agent := Agent{
		Client: chatFunc(func(_ context.Context, messages []llm.Message, definitions []llm.Tool) (llm.Message, error) {
			round++
			if round == 1 {
				if !hasTool(definitions, "control_home") {
					t.Fatal("agent lacks control_home tool")
				}
				if !strings.Contains(messages[0].Content, "control_home") {
					t.Fatal("agent system prompt lacks home-tool guidance")
				}
				return llm.Message{ToolCalls: []llm.ToolCall{{
					ID:   "home",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "control_home",
						Arguments: `{"request":"я нахожусь в спальне и ничего не вижу, помоги мне"}`,
					},
				}}}, nil
			}
			if last := messages[len(messages)-1]; last.Role != "tool" || last.ToolCallID != "home" || !strings.Contains(last.Content, "Готово") {
				t.Fatalf("home result missing: %+v", last)
			}
			return llm.Message{Content: "Включил свет в спальне."}, nil
		}),
		Tools:     tools,
		MaxRounds: 2,
		MaxCalls:  2,
	}
	answer, err := agent.Complete(t.Context(), []llm.Message{{Role: "user", Content: "я нахожусь в спальне и ничего не вижу, помоги мне"}}, false)
	if err != nil || answer != "Включил свет в спальне." || round != 2 {
		t.Fatalf("answer=%q rounds=%d error=%v", answer, round, err)
	}
}

func hasTool(tools []llm.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}
