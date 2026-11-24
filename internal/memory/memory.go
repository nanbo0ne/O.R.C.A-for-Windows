package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

// Set is everything memory loaded for one session: the hierarchical docs and a
// handle to the auto-memory store (whose index is captured at load time). It is
// assembled once at boot and folded into the system prompt by Compose. CWD and
// UserDir are retained so the controller can resolve quick-add targets without
// re-deriving discovery context.
type Set struct {
	Docs           []Source // DEEPSEEK_ORCA.md / AGENTS.md, ascending precedence
	Store          Store    // active writable auto-memory store (may be a zero/disabled Store)
	SharedStore    Store    // ordinary coding-agent memory store
	AssistantStore Store    // assistant-mode memory store
	Index          string   // MEMORY.md contents visible to this session at load time
	CWD            string   // project working dir used for discovery
	UserDir        string   // user config root (may be "")
	Profile        string   // assistant, shared-agent, or all
}

// Options configures discovery. CWD defaults to "." and UserDir is the user
// config root (config.MemoryUserDir()); a "" UserDir disables user-global docs
// and the auto-memory store.
type Options struct {
	CWD     string
	UserDir string
	Profile string
	// AssistantStoreDir overrides the assistant profile store. Product shells
	// use this to share one assistant identity across multiple workspaces.
	AssistantStoreDir string
}

const (
	ProfileSharedAgent = "shared-agent"
	ProfileAssistant   = "assistant"
	ProfileAll         = "all"
)

type ProfiledMemory struct {
	Memory
	Profile string
}

func NormalizeProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case ProfileAssistant:
		return ProfileAssistant
	case ProfileAll:
		return ProfileAll
	default:
		return ProfileSharedAgent
	}
}

// Load discovers all memory for a session: the hierarchical docs and the
// auto-memory index. It is best-effort and never errors — missing files just
// mean less memory — so boot can call it unconditionally.
func Load(opts Options) *Set {
	if config.IsolatedProfileEnabled() {
		opts.UserDir = config.MemoryUserDir()
		if opts.AssistantStoreDir != "" && config.ValidateProfileDataPath(opts.AssistantStoreDir) != nil {
			opts.AssistantStoreDir = ""
		}
	}
	cwd := opts.CWD
	if cwd == "" {
		cwd = "."
	}
	profile := NormalizeProfile(opts.Profile)
	sharedStore := StoreFor(opts.UserDir, cwd)
	assistantStore := AssistantStoreFor(opts.UserDir, cwd)
	if strings.TrimSpace(opts.AssistantStoreDir) != "" {
		assistantStore = Store{Dir: filepath.Clean(opts.AssistantStoreDir)}
	}
	store := sharedStore
	docs := discoverDocs(cwd, opts.UserDir)
	index := sharedStore.Index()
	if profile == ProfileAssistant {
		store = assistantStore
		docs = nil
		index = assistantStore.Index()
	} else if profile == ProfileAll {
		index = combineIndexes(sharedStore.Index(), assistantStore.Index())
	}
	return &Set{
		Docs:           docs,
		Store:          store,
		SharedStore:    sharedStore,
		AssistantStore: assistantStore,
		Index:          index,
		CWD:            cwd,
		UserDir:        opts.UserDir,
		Profile:        profile,
	}
}

func combineIndexes(shared, assistant string) string {
	shared = strings.TrimSpace(shared)
	assistant = strings.TrimSpace(assistant)
	if shared == "" {
		if assistant == "" {
			return ""
		}
		return "# Assistant memories\n\n" + assistant
	}
	if assistant == "" {
		return shared
	}
	return "# Shared agent memories\n\n" + shared + "\n\n# Assistant memories\n\n" + assistant
}

// DocPath returns the doc-memory file a given scope writes to. To avoid splitting
// a project's memory across conventions, it prefers a file that already exists
// (DEEPSEEK_ORCA.md / AGENTS.md / CLAUDE.md, in that order); when none exists it
// creates the universal default (AGENTS.md / AGENTS.local.md). ScopeUser →
// <userDir>, ScopeLocal → <cwd> with the *.local.md names, anything else → <cwd>.
// Returns "" for ScopeUser when no user dir is configured.
func (s *Set) DocPath(scope Scope) string {
	if config.IsolatedProfileEnabled() {
		if scope == ScopeUser {
			return filepath.Join(config.MemoryUserDir(), defaultDocName)
		}
		if scope == ScopeLocal {
			return config.ProjectStatePath(s.CWD, defaultLocalName)
		}
		return config.ProjectStatePath(s.CWD, defaultDocName)
	}
	dir := s.CWD
	names, def := docNames, defaultDocName
	switch scope {
	case ScopeUser:
		if s.UserDir == "" {
			return ""
		}
		dir = s.UserDir
	case ScopeLocal:
		names, def = localNames, defaultLocalName
	}
	for _, n := range names {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p // append to the doc already in use
		}
	}
	return filepath.Join(dir, def)
}

// Empty reports whether the set carries nothing to inject, so Compose can leave
// the base prompt byte-for-byte untouched (and the cache prefix maximal) when
// there is no memory at all.
func (s *Set) Empty() bool {
	return s == nil || (len(s.Docs) == 0 && strings.TrimSpace(s.Index) == "")
}

// docScopes are the scopes the panel can target for a quick-add or a new doc.
// Ordered broad → specific for display.
var docScopes = []Scope{ScopeUser, ScopeProject, ScopeLocal}

// allowedDocPaths is the closed set of files WriteDoc / AppendDoc may touch: the
// canonical file for each writable scope, plus every doc already discovered this
// session (so an ancestor or AGENTS.md the user is already editing stays
// editable). Keyed by absolute path. This bounds frontend-driven writes to real
// memory files rather than arbitrary paths.
func (s *Set) allowedDocPaths() map[string]bool {
	allow := map[string]bool{}
	for _, sc := range docScopes {
		if p := s.DocPath(sc); p != "" {
			allow[absOf(p)] = true
		}
	}
	for _, d := range s.Docs {
		allow[absOf(d.Path)] = true
	}
	return allow
}

// WriteDoc overwrites a doc-memory file with body, after checking path is a
// recognized memory file (see allowedDocPaths). It is the save side of the
// desktop panel's in-place editor. The write lands on disk immediately but does
// NOT mutate the cache-stable system prefix — the edit folds into the prefix on
// the next session; to make it apply this session, the controller separately
// queues a turn-tail note. Returns the path written.
func (s *Set) WriteDoc(path, body string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("memory unavailable")
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("no path given")
	}
	if !s.allowedDocPaths()[absOf(path)] {
		return "", fmt.Errorf("refusing to write %q: not a recognized memory file", path)
	}
	return path, writeDocFile(path, body)
}

// Block renders the memory as a single Markdown section, or "" when empty. It is
// deterministic given the same files, which is what keeps it a stable cache
// prefix across sessions that don't change their memory.
func (s *Set) Block() string {
	if s.Empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Memory\n\n")
	if s.Profile == ProfileAssistant {
		b.WriteString("Persistent context loaded from assistant-mode memory. Treat it as durable background from prior assistant-mode conversations.\n")
	} else {
		b.WriteString("Persistent context loaded from memory files. Treat it as durable, user-authored guidance for this project.\n")
	}

	for _, d := range s.Docs {
		fmt.Fprintf(&b, "\n## %s (%s)\n\n%s\n", d.Path, d.Scope, strings.TrimSpace(d.Body))
	}

	if idx := strings.TrimSpace(s.Index); idx != "" {
		b.WriteString("\n## Saved memories\n\n")
		b.WriteString("Facts you saved in earlier sessions. They reflect what was true when written and may now be stale — treat them as background, not standing instructions. " +
			"Read the linked file with read_file when one looks relevant, and before acting on one that names a file, function, or flag, verify it still exists. " +
			"Save new durable facts with the `remember` tool; delete ones that turn out wrong with `forget`.\n\n")
		b.WriteString(idx)
		fmt.Fprintf(&b, "\n\n(stored under %s)\n", s.Store.Dir)
	}
	return b.String()
}

// AssistantRecallBlock renders assistant-mode memories for per-turn recall. It
// includes the full index and then as many memory bodies as fit in bodyCharBudget
// so assistant mode can make relevance decisions without forcing the user to ask
// for a specific memory file.
func (s *Set) AssistantRecallBlock(bodyCharBudget int) string {
	if s == nil || s.Profile != ProfileAssistant {
		return ""
	}
	index := strings.TrimSpace(s.AssistantStore.Index())
	memories := s.AssistantStore.List()
	if index == "" && len(memories) == 0 {
		return ""
	}
	if bodyCharBudget <= 0 {
		bodyCharBudget = 12000
	}
	var b strings.Builder
	b.WriteString("# Assistant Memories\n\n")
	b.WriteString("Persistent assistant-mode memory from prior Orca conversations. Use only the parts relevant to the user's current request. Do not mention the memory system unless the user asks why you know something.\n")
	if index != "" {
		b.WriteString("\n## Complete index\n\n")
		b.WriteString(index)
		b.WriteString("\n")
	}
	if len(memories) == 0 {
		return b.String()
	}
	sort.SliceStable(memories, func(i, j int) bool {
		ai := memories[i].UpdatedAt
		if ai == "" {
			ai = memories[i].CreatedAt
		}
		aj := memories[j].UpdatedAt
		if aj == "" {
			aj = memories[j].CreatedAt
		}
		if ai != aj {
			return ai > aj
		}
		if memories[i].Confidence != memories[j].Confidence {
			return memories[i].Confidence > memories[j].Confidence
		}
		return memories[i].Name < memories[j].Name
	})
	b.WriteString("\n## Memory details\n\n")
	used := 0
	omitted := 0
	for _, m := range memories {
		body := strings.TrimSpace(m.Body)
		if body == "" {
			continue
		}
		var item strings.Builder
		title := displayTitle(m.Title, m.Name)
		fmt.Fprintf(&item, "### %s (%s)\n", title, m.Name)
		if desc := oneLine(m.Description); desc != "" {
			fmt.Fprintf(&item, "Description: %s\n", desc)
		}
		if m.Type != "" {
			fmt.Fprintf(&item, "Type: %s\n", NormalizeType(string(m.Type)))
		}
		if m.Confidence > 0 {
			fmt.Fprintf(&item, "Confidence: %.2f\n", m.Confidence)
		}
		item.WriteString("\n")
		item.WriteString(body)
		item.WriteString("\n\n")
		text := item.String()
		if used+len(text) > bodyCharBudget {
			omitted++
			continue
		}
		b.WriteString(text)
		used += len(text)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "_%d memory detail(s) omitted because the recall block hit its size budget; use the complete index above as compact background._\n", omitted)
	}
	return strings.TrimSpace(b.String())
}

// ListProfiled returns saved auto-memories from every local memory profile so
// the desktop panel can classify them without changing the active prompt scope.
func (s *Set) ListProfiled() []ProfiledMemory {
	if s == nil {
		return nil
	}
	var out []ProfiledMemory
	for _, m := range s.SharedStore.List() {
		out = append(out, ProfiledMemory{Memory: m, Profile: ProfileSharedAgent})
	}
	for _, m := range s.AssistantStore.List() {
		out = append(out, ProfiledMemory{Memory: m, Profile: ProfileAssistant})
	}
	return out
}

// Compose folds the memory block onto the base system prompt and returns the
// durable cached-prefix string. Base stays first (it is the most stable text, so
// it remains a valid cache prefix even when memory changes between sessions);
// memory follows. With no memory, base is returned unchanged.
func Compose(base string, s *Set) string {
	block := s.Block()
	if block == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return block
	}
	return strings.TrimRight(base, "\n") + "\n\n" + block
}
