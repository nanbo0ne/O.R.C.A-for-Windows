package webbeta

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/promptprofile"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestTransportContractLegacyBytesUnchanged(t *testing.T) {
	for _, test := range []struct {
		name    string
		grammar int
		tools   bool
		fresh   bool
		want    string
	}{
		{"v2-no-tools-fresh", 2, false, true, "2d2762dd256ea8933d7a8f2861370612f7bcf21eceff1668e577e7f0061bec49"},
		{"v2-no-tools-incremental", 2, false, false, "ddcd974609ded75421a5c2af9878e8b9679bac1361f5385fdde916afc2b7c00e"},
		{"v1-fresh", 1, true, true, "e7cb3f67bdbcace11487110d4dd774c3ecc0cb31782873bac1ce78226b7b342b"},
		{"v1-incremental", 1, true, false, "f99fcae21cc1c679ea7e4b2eeb42873cf359f92cc05cd33dbee699dee9afd2bd"},
		{"v0-fresh", 0, true, true, "b3ae95fd330da0645e06eed161324111080208ee20a737dfbe5d01beb79ade65"},
		{"v0-incremental", 0, true, false, "c83d7aa9d849bfcbbe22224d6574a225ccf196157ac4256ab585ea1daa72e5fb"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var tools []provider.ToolSchema
			if test.tools {
				tools = []provider.ToolSchema{{Name: "synthetic_echo"}}
			}
			text := render([]provider.Message{{Role: provider.RoleSystem, Content: "synthetic system"}, {Role: provider.RoleUser, Content: "synthetic request"}}, tools, test.grammar, test.fresh)
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
			if hash != test.want {
				t.Fatalf("baseline %s", hash)
			}
		})
	}
}

func TestTransportContractFollowsHistoryAndPreservesModePrompt(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		for _, system := range []string{
			promptprofile.CodingSystemPrompt("", "", "", "", "", ""),
			promptprofile.AssistantSystemPrompt("", "", "", "", ""),
			promptprofile.OrcaSystemPrompt("", "", "", "", ""),
		} {
			messages := []provider.Message{{Role: provider.RoleSystem, Content: system}, {Role: provider.RoleUser, Content: "synthetic question"}, {Role: provider.RoleTool, ToolCallID: "synthetic-call", Name: "synthetic_echo", Content: "synthetic result"}}
			text := render(messages, []provider.ToolSchema{{Name: "synthetic_echo"}}, currentGrammar, fresh)
			marker := "网页接入传输格式补充"
			if strings.Count(text, marker) != 1 || strings.Index(text, marker) <= strings.Index(text, "synthetic result") {
				t.Fatal("missing unique trailing contract")
			}
			if !strings.Contains(text, system) || strings.Index(text, system) >= strings.Index(text, "synthetic question") {
				t.Fatal("mode prompt or message order changed")
			}
			if strings.Count(text, protocolInstructions()) != 1 || !strings.Contains(text, `"tool_call_id":"synthetic-call"`) {
				t.Fatal("existing protocol/result identity changed")
			}
			if !strings.HasSuffix(text, transportContract) {
				t.Fatal("contract not at end of payload")
			}
		}
	}
}

func TestTransportContractIncrementalUserAndToolReplies(t *testing.T) {
	for _, messages := range [][]provider.Message{
		{{Role: provider.RoleUser, Content: "just the answer"}},
		{{Role: provider.RoleTool, Name: "synthetic_echo", ToolCallID: "owned-id", Content: "synthetic returned value"}},
	} {
		text := render(messages, []provider.ToolSchema{{Name: "synthetic_echo"}}, currentGrammar, false)
		if !strings.HasSuffix(text, transportContract) || strings.Count(text, transportContract) != 1 {
			t.Fatal("incremental contract missing or duplicated")
		}
		if strings.Count(text, messages[0].Content) != 1 || strings.Contains(text, "[system]") {
			t.Fatal("incremental history rewritten")
		}
	}
}

func TestTransportContractDoesNotRelaxResponseParsing(t *testing.T) {
	tools := []provider.ToolSchema{{Name: "synthetic_echo"}}
	for _, body := range []string{
		"57", "A natural-language answer.",
		"```json\nORCA_TURN_V2 {\"type\":\"final\",\"text\":\"57\"}\n```",
		"> ORCA_TURN_V2 {\"type\":\"tools\",\"calls\":[{\"id\":\"quoted\",\"name\":\"synthetic_echo\",\"arguments\":{}}]}",
	} {
		if _, err := parseTurn(body, tools, nil, currentGrammar); err == nil {
			t.Fatalf("accepted non-record response %q", body)
		}
		if repairableTurn(body, tools, nil) {
			t.Fatalf("non-record became automatically repairable: %q", body)
		}
	}
	turn, err := parseTurn(`ORCA_TURN_V2 {"type":"final","text":"A natural-language answer."}`, tools, nil, currentGrammar)
	if err != nil || turn.Final != "A natural-language answer." || len(turn.Calls) != 0 {
		t.Fatalf("ordinary framed answer failed: %+v %v", turn, err)
	}
	if strings.Contains(transportContract, `<host_context`) {
		t.Fatal("transport must not invent a platform trust marker")
	}
}
