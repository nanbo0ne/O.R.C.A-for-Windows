# Conversation UI and Work Monitor: Round 1

Date: 2026-09-23

## 本轮结论

- 本轮只做本地修复、自动测试和编译，未提交、推送、发版或更新线上下载源。
- 压缩后保留可见原文，增加可展开摘要的分隔标记；不会把全部旧历史重新发送给模型。
- 新增左侧工作监控和事件驱动色带，修复 Todo/下箭头遮挡、滚动跳位及引导消息排版。
- 当前上下文、最近请求和累计用量分开；缺失 reasoning 回执显示“未提供”，不伪装成零。
- 测试使用合成数据，未读取用户聊天文件，未进行电脑操控，未关闭或替换正在运行的应用。
- 旧版本已经丢失的原文无法凭空恢复；原生窗口交互、真实供应商和安装升级留待后续验收。
- 以下记录区分已执行的检查、已知限制和下一轮事项，不代表正式发行验收已完成。

Status: local implementation and verification only. No release, tag, push,
installer, update manifest, deployment, or version increment in this round.
Working tree: `D:\AI-Reasonix\.tmp\v2.1.3-worktree`, branch `codex/v3.0.0`.

## Changes

### Compression without losing visible conversation

- Model context and visible history are separate. Compression and tool-result
  pruning only shorten model context; original messages remain in the transcript.
- A lightweight `上下文已压缩` divider marks the time of compression; its summary
  expands independently. Repeated compression does not replace older dividers.
- Stable message identities survive saving, reopening, and controller rebuilding.
  Guidance messages remain in their parent turn, including repeated identical text.
- Both histories are saved in one JSONL file, with optional metadata on the first
  message. Older JSON message readers remain able to read model context. Older
  applications that rewrite this file do not understand or preserve the new metadata;
  downgrading is not a supported way to preserve the new visible transcript.
- Archive names cannot collide within a millisecond. All removed originals,
  including extra tail trimming, are covered by the archive.
- Save uses a complete synced sibling file. If replacement fails, the original is
  not truncated; the error identifies the complete recovery file. Filter-driver or
  permission failures are reported, not silently bypassed with an unsafe copy.
- Compaction cancellation and failure leave original visible history intact.
  A no-op compact does not erase valid checkpoint boundaries.
- Context epochs invalidate obsolete conversation indexes after automatic/manual
  compression and restart. File snapshots are not deleted. An unavailable old
  conversation rewind cannot partially execute a file restore first.
- Mode-switch records now anchor to display-message IDs, rather than compacted
  context offsets. Deleted branch anchors do not attach to unrelated messages.
- Legacy files whose original messages were already removed show an explicit
  missing-original notice. This round does not read personal archives or attempt
  speculative reconstruction.

### Work monitor

- `工作监控` is above History, Trash, and Settings. Its pane sits in the same sidebar
  column, above those navigation actions, without covering the conversation list.
- Default pane height is 320px with pointer/keyboard resizing. The phase ribbon is
  44px. Container limits preserve navigation visibility and conversation-list space.
- Captures the actual encoded outbound request, returned post-Hook text/reasoning,
  tool arguments/results, usage receipts, and observed child lifecycle events.
- Request and retry-attempt identities are separate. Responses without request IDs
  are labeled turn-scoped, not falsely associated with one particular HTTP attempt.
- Only the currently visible pane captures. Closing, hiding the document, leaving
  the tab, entering settings/history, and responsive sidebar collapse unsubscribe.
  A renewable lease clears abandoned backend observation. No monitor persistence.
- Capture is bounded and filtered. Images, authentication data, original reasoning
  replay, and recognized credential patterns are withheld. Truncation, eviction,
  and dropped records are visible. Arbitrary embedded secrets cannot be guaranteed
  detectable; this is not an unfiltered network capture or a credential auditor.
- Structured response/tool fragments also filter credential keys and quoted
  credential assignments. Secrets split across stream fragments may not be
  recognizable. Retry/failure/cancellation add an explicit incomplete diagnostic
  without copying raw provider errors, credentials, or endpoint URLs.
- A flat colored ribbon appends actual input/wait/reasoning/output/tool/pause/
  cancellation/final/stopped/error phases, with elapsed times. It does not invent
  timed transitions. Unknown prefill is labeled waiting for the first fragment.
- Content and ribbon have independent follow controls. Opening the monitor mid-turn
  cannot recover earlier unobserved events. Tool/child counts describe retained
  observed events, not an authoritative scheduler-wide count.
- Foreground task children now emit paired start/end events too, including on
  failure/cancellation, and associate usage receipts with the same child identity.
  Background preparation failures do not create an unmatched start event.
- Existing responsive policy below 1120px still collapses the sidebar and disables
  capture; this round does not redesign small-screen navigation.
- At heights below 560px the monitor closes and unsubscribes instead of forcing
  its minimum height over navigation. Synthetic 480px/180px cases verify cleanup.

See `docs/work-monitor.md` for capture limits, hooks, and test fixtures.

### Conversation scrolling, Todo and guidance

- The down-arrow position and bottom space account for both the compact Todo bar
  and its expanded popover. No full-width opaque Todo overlay was added.
- One small upward wheel movement within 128px has a 180ms return tolerance;
  continued or larger upward movement leaves follow mode for ordinary reading.
- Viewport anchors are not restored using stale coordinates after native scrolling.
  Earlier history can be paged in without collapsing the visible conversation.
- Guidance is a normal user-style bubble with a muted label, outside process folds.
  Completed activity before and after guidance/compaction folds while final answers
  remain visible. A completed turn has only one totals row.

### Token usage

- Current context occupancy, latest request, and cumulative session usage are three
  separate sections. Cumulative millions of tokens no longer masquerade as context.
- Explicit zero reasoning and missing reasoning are distinct. Missing receipts show
  `未提供`, not a fabricated zero; incomplete cumulative reasoning is marked partial.
- Negative or out-of-range reasoning receipts are unavailable, not clamped into
  made-up precise counts. Valid explicit zero remains available.
- Reasoning is a subset of completion usage. Visible output subtracts reasoning
  only when the provider supplies it, and total usage does not add reasoning twice.
- This does not infer hidden reasoning, token IDs, a provider's internal prefill
  progress, or an unreported future reasoning budget from displayed text.

## Verification

All fixtures use synthetic messages and temporary directories. No stored user
history, personal configuration, API credentials, or service logs were read.
Supplied UI screenshots are visual references only, not test inputs sent to a
provider. No production model requests or Computer Use were performed.

| Check | Result |
| --- | --- |
| Root `go test ./... -p=2 -count=1` | Passed |
| Desktop `go test . -count=1` | Passed |
| Frontend `npm run test:all` including streaming, turn controls and conversation UI | Passed |
| Frontend `npm run build` including CSS and TypeScript checks | Passed; existing large-chunk warning remains |
| Windows Wails production compilation, separate local filename, no launch/installer | Passed; regenerated bindings, existing `time.Time` binding warning remains |
| Display-history, checkpoint epoch, branch and mode-switch regressions | Passed |
| Monitor provider wire-shape, privacy, lifecycle, bounds and concurrent access tests | Passed |
| Monitor parser fuzz | Worker ran approximately 89,900 executions plus deterministic malformed/Unicode/depth fixtures; passed |
| Full-shell isolated browser suite | 56 assertions, 8 Modern/Classic width cases, 10 screenshots; passed |
| Dedicated monitor interaction suite, independently rerun by main worker | 52 assertions, 4 expanded layouts, responsive collapse, short heights, hide, resize and follow; passed |
| Transcript guidance, compaction, Todo and scrolling browser suite | 162 assertions passed, including manual return-to-bottom and upward-scroll/resize races |
| `git diff --check` | Passed; only repository line-ending conversion notices |

Final rerun completed after the privacy, interruption, child-lifecycle, and invalid
usage follow-ups: both Go suites, frontend tests/build, monitor browser checks and
Windows compilation passed. The isolated Vite test server was stopped afterward.

Visual evidence (local, not published):

- `desktop/frontend/.tmp/workspace-monitor-evidence/modern-1366-monitor.png`
- `desktop/frontend/.tmp/workspace-monitor-evidence/classic-1366-monitor.png`
- `desktop/frontend/.tmp/workspace-monitor-evidence/modern-1920-context-usage.png`
- `.tmp/transcript-ui-evidence/modern-guidance-compaction.png`
- `.tmp/transcript-ui-evidence/classic-guidance-compaction.png`
- `.tmp/transcript-ui-evidence/modern-compaction-summary-open.png`
- `.tmp/transcript-ui-evidence/modern-760-todo.png`
- `.tmp/work-monitor-evidence/`

Main worker inspected rendered screenshots, not just source contracts. A first
monitor geometry test missed viewport containment despite passing; screenshot
review caught the off-screen navigation. The test now checks containment, usable
pane height, and the status-bar boundary, and the layout was corrected before the
passing result above.

## Remaining Limits / Next Round

- Native Windows Wails interaction and system DPI were not exercised. The running
  user's application was not closed, upgraded, or replaced.
- The local compile-check executable uses an explicitly separate filename. It is
  not a packaged trial release and must not be uploaded as an official asset.
- Go race detection is unavailable here: CGO is disabled and no gcc/clang compiler
  is available. Concurrency regression tests passed without the race detector.
- No live third-party/provider compatibility, installation-upgrade, signing, or
  public download checks are claimed for this unreleased iteration.
- Original transcripts already lost by older versions are not automatically restored.
- Frontend bundle size still produces a build warning; broad bundle refactoring is
  outside this focused round.
- Existing unrelated community data and prior audit files remain untouched.
- Review the local changes in the next round before deciding on versioning,
  installer creation, or release. Current published 3.0.12 remains unchanged.
