//go:build windows && production && webbeta_live_memory

package webbetahost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool/builtin"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

type lfCase struct {
	kind, dir, goal string
	values          []int
	checks          int
	mu              sync.Mutex
}

func lfSetup(root, kind string, values []int) (*lfCase, error) {
	if kind == "file-csv-no-thinking" {
		kind = "file-csv"
	}
	dir := filepath.Join(root, "synthetic-project")
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	c := &lfCase{kind: kind, dir: dir, values: values}
	files := map[string]string{}
	switch kind {
	case "file-code":
		files["go.mod"] = "module synthetic-task\n\ngo 1.25\n"
		files["calc.go"] = "package task\n\nfunc SumEven(nums []int) int {\n total := 0\n for _, n := range nums {\n  if n%2 != 0 { total += n }\n }\n return total\n}\n"
		files["calc_test.go"] = fmt.Sprintf("package task\nimport \"testing\"\nfunc TestSumEven(t *testing.T) {\n for _, tc := range []struct{in []int; want int}{\n {[]int{%d,%d,%d},%d},\n {[]int{2,3,4,5},6},\n {[]int{-2,-3,8},6},\n } { if got:=SumEven(tc.in); got!=tc.want { t.Fatalf(\"got %%d want %%d\",got,tc.want) } }\n}\n", values[0], values[1], values[2], lfEvenSum(values[:3]))
		c.goal = "这个合成 Go 项目的 SumEven 有缺陷。检查 calc.go 与 calc_test.go，修复函数并运行项目校验。不要改测试文件。"
	case "file-csv":
		files["orders.csv"] = fmt.Sprintf("category,amount\nA,%d\nB,%d\nA,%d\nB,%d\n", values[0], values[1], values[2], values[3])
		files["README.txt"] = "统计 orders.csv：按类别求和，输出 report.json，格式为 {\"totals\":{\"A\":整数,\"B\":整数},\"grand\":整数}。完成后运行项目校验。\n"
		c.goal = "读取这个合成项目的说明和订单数据，制作准确的汇总报告并运行项目校验。"
	case "file-cross":
		files["instructions.txt"] = "读取 alpha.txt 和 beta.txt。把两份文件中的 code 按 alpha-beta 顺序用冒号连接，并将两份文件中的 score 相加，输出 result.json：{\"code\":字符串,\"total\":整数}。完成后运行项目校验。\n"
		files["alpha.txt"] = fmt.Sprintf("code=A%d\nscore=%d\n", values[0], values[1])
		files["beta.txt"] = fmt.Sprintf("code=B%d\nscore=%d\n", values[2], values[3])
		c.goal = "根据合成项目的说明和两个资料文件生成最终产物，随后运行项目校验。"
	default:
		return nil, errors.New("unknown synthetic file case")
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func lfEvenSum(nums []int) int {
	sum := 0
	for _, n := range nums {
		if n%2 == 0 {
			sum += n
		}
	}
	return sum
}

type lfCheckTool struct{ c *lfCase }

func (*lfCheckTool) Name() string { return "check_task" }
func (*lfCheckTool) Description() string {
	return "Run the isolated project's deterministic acceptance check and report PASS or a bounded failure."
}
func (*lfCheckTool) ReadOnly() bool { return true }
func (*lfCheckTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t *lfCheckTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if strings.TrimSpace(string(raw)) != "{}" {
		return "", errors.New("check_task takes no arguments")
	}
	t.c.mu.Lock()
	t.c.checks++
	t.c.mu.Unlock()
	if err := lfVerifyProject(ctx, t.c); err != nil {
		return "FAIL: " + err.Error(), nil
	}
	return "PASS: project output independently verified", nil
}

func lfVerifyProject(ctx context.Context, c *lfCase) error {
	switch c.kind {
	case "file-csv", "file-cross":
		name := "report.json"
		if c.kind == "file-cross" {
			name = "result.json"
		}
		raw, err := os.ReadFile(filepath.Join(c.dir, name))
		if err != nil {
			return errors.New("expected output file missing")
		}
		var got map[string]json.RawMessage
		if json.Unmarshal(raw, &got) != nil || len(got) != 2 {
			return errors.New("output JSON has invalid shape")
		}
		if c.kind == "file-csv" {
			var totals map[string]int
			var grand int
			if json.Unmarshal(got["totals"], &totals) != nil || json.Unmarshal(got["grand"], &grand) != nil || len(totals) != 2 || totals["A"] != c.values[0]+c.values[2] || totals["B"] != c.values[1]+c.values[3] || grand != c.values[0]+c.values[1]+c.values[2]+c.values[3] {
				return errors.New("CSV totals do not match the source")
			}
			return nil
		}
		var code string
		var total int
		if json.Unmarshal(got["code"], &code) != nil || json.Unmarshal(got["total"], &total) != nil || code != fmt.Sprintf("A%d:B%d", c.values[0], c.values[2]) || total != c.values[1]+c.values[3] {
			return errors.New("cross-file result does not match both sources")
		}
		return nil
	case "file-code":
		source, err := parser.ParseFile(token.NewFileSet(), filepath.Join(c.dir, "calc.go"), nil, parser.AllErrors)
		if err != nil || source.Name.Name != "task" || len(source.Decls) != 1 {
			return errors.New("calc.go is not a single valid task function")
		}
		fn, ok := source.Decls[0].(*ast.FuncDecl)
		if !ok || fn.Name.Name != "SumEven" || fn.Recv != nil {
			return errors.New("SumEven signature or declarations changed")
		}
		unsafe := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.CallExpr, *ast.GoStmt, *ast.DeferStmt, *ast.SelectStmt, *ast.FuncLit:
				unsafe = true
			}
			return true
		})
		if unsafe {
			return errors.New("calc.go contains out-of-scope operations")
		}
		testCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		goExe := os.Getenv("ORCA_TEST_GO")
		if goExe == "" || !filepath.IsAbs(goExe) {
			return errors.New("isolated Go test runtime unavailable")
		}
		cmd := exec.CommandContext(testCtx, goExe, "test", "./...")
		cmd.Dir = c.dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
		if _, err := cmd.CombinedOutput(); err != nil {
			return errors.New("Go tests failed or timed out")
		}
		return nil
	}
	return errors.New("unknown synthetic verifier")
}

type lfGate struct{ root string }

func (g lfGate) Check(ctx context.Context, name string, raw json.RawMessage, _ bool) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "cancelled", err
	}
	if name == "check_task" {
		return true, "", nil
	}
	if name != "read_file" && name != "write_file" && name != "ls" {
		return false, "outside isolated project tools", nil
	}
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(raw, &args) != nil {
		return false, "invalid path", nil
	}
	if name == "ls" && args.Path == "" {
		args.Path = "."
	}
	if args.Path == "" {
		return false, "missing path", nil
	}
	target := args.Path
	if !filepath.IsAbs(target) {
		target = filepath.Join(g.root, target)
	}
	if target != g.root && !lmUnder(g.root, target) {
		return false, "outside isolated project", nil
	}
	if err := rejectLinks(target); err != nil {
		return false, "linked paths are unavailable", nil
	}
	return true, "", nil
}

type lfTrace struct {
	mu       sync.Mutex
	commits  int
	answer   string
	dispatch map[string]event.Tool
	results  map[string]event.Tool
	bad      bool
}

func (s *lfTrace) Emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Kind {
	case event.AnswerCommitted:
		s.commits++
		s.answer = ev.Text
	case event.ToolDispatch:
		if !ev.Tool.Partial {
			if _, ok := s.dispatch[ev.Tool.ID]; ok {
				s.bad = true
			}
			s.dispatch[ev.Tool.ID] = ev.Tool
		}
	case event.ToolResult:
		if _, ok := s.results[ev.Tool.ID]; ok {
			s.bad = true
		}
		s.results[ev.Tool.ID] = ev.Tool
	case event.ApprovalRequest, event.AskRequest:
		s.bad = true
	}
}

func lmPureQA(ctx context.Context, wire *lmBrowser, memory *lmMemory, report *lmReport) error {
	report.Scope = "native WebView2/Engine/Adapter/Agent; pure arithmetic with tools available but not needed"
	reg := tool.NewRegistry()
	for _, name := range lmNames {
		reg.Add(&lmTool{name: name, memory: memory})
	}
	report.Registry = append([]string(nil), lmNames...)
	base, err := provider.New("deepseekweb-beta", provider.Config{Name: "synthetic-pure-qa-web-only"})
	if err != nil {
		return err
	}
	tasks, ok := base.(provider.TaskProvider)
	if !ok {
		return errors.New("missing web task provider")
	}
	p := &lmProvider{Provider: base, tasks: tasks, reg: reg}
	trace := &lfTrace{dispatch: map[string]event.Tool{}, results: map[string]event.Tool{}}
	a := agent.New(p, reg, agent.NewSession("纯问答时不要调用不必要的工具。"), agent.Options{MaxSteps: 3, Gate: lmGate{memory.cancel}}, trace)
	report.Stage = "pure_qa_agent_task"
	task, cancel := context.WithTimeout(ctx, 4*time.Minute)
	err = a.Run(task, "请直接回答：7 加 5 等于几？这是纯数学常识，不需要读取合成记录。")
	cancel()
	p.mu.Lock()
	report.ProviderRequests = len(p.requests)
	p.mu.Unlock()
	if err != nil {
		return err
	}
	trace.mu.Lock()
	commits, answer, bad, dispatches := trace.commits, trace.answer, trace.bad, len(trace.dispatch)
	trace.mu.Unlock()
	if commits != 1 || !strings.Contains(answer, "12") || bad || dispatches != 0 || report.ProviderRequests != 1 {
		return errors.New("pure question used unnecessary tools or failed to answer")
	}
	memory.mu.Lock()
	attempts := len(memory.attempts)
	memory.mu.Unlock()
	if attempts != 0 {
		return errors.New("pure question executed a memory tool")
	}
	wire.mu.Lock()
	receipts := len(wire.receipts)
	wire.mu.Unlock()
	if receipts != 0 {
		return errors.New("pure question forwarded unexpected tool receipts")
	}
	report.Stage = "pure_qa_complete"
	return nil
}

func lfExercise(ctx context.Context, root string, engine *webbeta.Engine, wire *lmBrowser, memory *lmMemory, report *lmReport) error {
	project, err := lfSetup(root, report.Variant, memory.record.Values)
	if err != nil {
		return err
	}
	report.Scope = "native WebView2/Engine/Adapter/Agent; real confined project file tools; synthetic inputs only"
	reg := tool.NewRegistry()
	for _, item := range (builtin.Workspace{Dir: project.dir}).Tools("ls", "read_file", "write_file") {
		reg.Add(item)
	}
	reg.Add(&lfCheckTool{project})
	names := reg.Names()
	report.Registry = append([]string(nil), names...)
	checkRegistry := func(r *tool.Registry) error {
		if !reflect.DeepEqual(r.Names(), names) || r.Len() != 4 {
			return errors.New("synthetic file registry changed")
		}
		return nil
	}
	base, err := provider.New("deepseekweb-beta", provider.Config{Name: "synthetic-project-web-only"})
	if err != nil {
		return err
	}
	tasks, ok := base.(provider.TaskProvider)
	if !ok {
		return errors.New("missing web task provider")
	}
	p := &lmProvider{Provider: base, tasks: tasks, reg: reg, registryCheck: checkRegistry, disableThinking: report.Variant == "file-csv-no-thinking"}
	trace := &lfTrace{dispatch: map[string]event.Tool{}, results: map[string]event.Tool{}}
	a := agent.New(p, reg, agent.NewSession("只处理当前隔离项目，不使用项目外资料。"), agent.Options{MaxSteps: 10, Gate: lfGate{project.dir}}, trace)
	report.Stage = "file_agent_task"
	report.TaskStartedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	task, cancel := context.WithTimeout(ctx, 5*time.Minute)
	err = a.Run(task, project.goal)
	cancel()
	report.TaskEndedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	p.mu.Lock()
	report.ProviderRequests = len(p.requests)
	requests := append([]provider.Request(nil), p.requests...)
	p.mu.Unlock()
	report.TaskScopesStarted, report.TaskScopesFinished = p.started.Load(), p.finished.Load()
	wire.mu.Lock()
	report.SyntheticWebResponses = append([]string(nil), wire.observed...)
	wire.mu.Unlock()
	project.mu.Lock()
	report.ProjectChecks = project.checks
	project.mu.Unlock()
	trace.mu.Lock()
	report.ProjectToolCalls = len(trace.dispatch)
	trace.mu.Unlock()
	if err != nil {
		return err
	}
	report.Stage = "file_independent_verification"
	verify, stop := context.WithTimeout(ctx, 15*time.Second)
	err = lfVerifyProject(verify, project)
	stop()
	if err != nil {
		report.VerificationIssue = err.Error()
		return err
	}
	project.mu.Lock()
	checks := project.checks
	project.mu.Unlock()
	trace.mu.Lock()
	commits, bad := trace.commits, trace.bad
	dispatch, results := trace.dispatch, trace.results
	trace.mu.Unlock()
	report.ProjectChecks, report.ProjectToolCalls = checks, len(dispatch)
	outputName := "result.json"
	if project.kind == "file-code" {
		outputName = "calc.go"
	}
	if project.kind == "file-csv" {
		outputName = "report.json"
	}
	output, err := os.ReadFile(filepath.Join(project.dir, outputName))
	if err != nil {
		return err
	}
	outputHash := sha256.Sum256(output)
	report.ProjectOutputSHA256 = hex.EncodeToString(outputHash[:])
	if checks == 0 || commits != 1 || bad || len(dispatch) < 3 || len(dispatch) != len(results) || p.started.Load() != 1 || p.finished.Load() != 1 || len(requests) < 3 || len(requests) > 10 {
		return errors.New("file task tool/check/turn counts failed")
	}
	seen := map[string]bool{}
	for _, msg := range a.Session().Snapshot() {
		if msg.Role != provider.RoleTool {
			continue
		}
		d, ok := dispatch[msg.ToolCallID]
		result, hasResult := results[msg.ToolCallID]
		if seen[msg.ToolCallID] || !ok || !hasResult || d.Name != msg.Name || result.Name != msg.Name || result.Output != msg.Content || result.Err != "" {
			return errors.New("file tool receipt differs from actual execution")
		}
		seen[msg.ToolCallID] = true
		wire.mu.Lock()
		count := 0
		for _, receipt := range wire.receipts {
			if receipt.ID == msg.ToolCallID && receipt.Name == msg.Name && receipt.Result == msg.Content {
				count++
			}
		}
		wire.mu.Unlock()
		if count != 1 {
			return errors.New("file tool receipt was not delivered exactly once to website")
		}
	}
	if len(seen) != len(dispatch) {
		return errors.New("file task has missing tool receipts")
	}
	report.Stage = "file_task_complete"
	return nil
}

func TestSyntheticFileFixtureVerifier(t *testing.T) {
	for _, kind := range []string{"file-code", "file-csv", "file-cross"} {
		c, err := lfSetup(t.TempDir(), kind, []int{2, 3, 4, 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := lfVerifyProject(context.Background(), c); err == nil {
			t.Fatalf("%s fixture unexpectedly starts complete", kind)
		}
	}
}
