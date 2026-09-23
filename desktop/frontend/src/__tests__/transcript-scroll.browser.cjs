// Isolated real React + production CSS. No app server, profile, or backend.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const { build } = require('esbuild');
const { chromium } = require('playwright');
const root = path.resolve(__dirname, '../..');
const output = path.resolve(root, '../../.tmp/transcript-ui-evidence');
const fixture = `
  import React from 'react';
  import { createRoot } from 'react-dom/client';
  import { flushSync } from 'react-dom';
  import { Transcript } from './src/components/Transcript';
  import { TodoPanel } from './src/components/TodoPanel';
  import { LocaleProvider } from './src/lib/i18n';
  import { initialState, reducer, historyMessagesToItems } from './src/lib/useController';
  const root = createRoot(document.getElementById('root'));
  let state = initialState, session = 0, todo = true;
  function render() { flushSync(() => root.render(<LocaleProvider><div id="fixture-frame">
    <main className={'main' + (todo ? ' main--has-todos' : '')}>
      <Transcript key={session} items={state.items} live={state.live} running={state.running}
        onPrompt={() => {}} activityIndicatorEnabled={false} />
    </main>
    <footer className="footer">{todo && <TodoPanel todoId="fixture-todo" todos={[
      {content:'Verify isolated fixture geometry', activeForm:'Verifying isolated fixture geometry', status:'in_progress'},
      {content:'Check continuous history', status:'pending'},
      {content:'Inspect browser evidence', status:'pending'}
    ]} onDismiss={() => {todo = false; render();}} />}</footer>
  </div></LocaleProvider>)); }
  window.fixture = {
    reset(items) { session++; todo = true; state = {...initialState, items, running:true, turnActive:true, currentTurnId:'live'}; render(); },
    grow(text=' More streamed content.') { state = reducer(state, {type:'event', e:{kind:'text', turnId:'live', messageId:'stream', text}}); render(); },
    append(items) { state = {...state, items:[...state.items,...items]}; render(); },
    hydrate(history) { this.reset(historyMessagesToItems(history, 'fixture').items); },
    todo(value) { todo = value; render(); }
  };
`;
const history = (count = 80) => Array.from({ length: count }, (_, i) => [
  { kind: 'user', id: `u${i}`, turnId: `turn${i}`, text: `Question ${i}: inspect the continuous history` },
  { kind: 'tool', id: `tool${i}`, turnId: `turn${i}`, name: 'read', args: '{}', status: 'done', readOnly: true },
  { kind: 'assistant', id: `a${i}`, turnId: `turn${i}`, text: `Answer ${i}. ` + 'Readable fixture text. '.repeat(22), reasoning: '', streaming: false, final: true },
  { kind: 'turn_stats', id: `s${i}`, turnId: `turn${i}`, success: true, outcome: 'success' },
]).flat().concat([
  { kind: 'user', id: 'live-user', turnId: 'live', text: 'Continue streaming here' },
  { kind: 'assistant', id: 'stream', turnId: 'live', messageId: 'stream', text: 'Streaming text. '.repeat(30), reasoning: '', streaming: true },
]);

(async () => {
  fs.mkdirSync(output, { recursive: true });
  const bundle = await build({ stdin: { contents: fixture, loader: 'tsx', resolveDir: root },
    tsconfig: path.join(root, 'tsconfig.json'), bundle: true, write: false, format: 'iife', platform: 'browser',
    loader: { '.css': 'empty', '.png': 'dataurl' }, define: { 'process.env.NODE_ENV': '"development"' }, logLevel: 'silent' });
  const browser = await chromium.launch({ headless: true, ignoreDefaultArgs: ['--hide-scrollbars'], executablePath: process.env.ORCA_BROWSER_EXECUTABLE ||
    (process.platform === 'win32' ? 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' : undefined) });
  const report = { assertions: [], measurements: [], errors: [], externalRequests: [], screenshots: [] };
  const check = (condition, label, data) => { report.assertions.push({ label, passed: !!condition, data }); assert.ok(condition, `${label}: ${JSON.stringify(data)}`); };
  try {
    const context = await browser.newContext({ locale: 'zh-CN', viewport: { width: 1280, height: 900 } });
    const page = await context.newPage();
    page.on('pageerror', e => report.errors.push(e.message));
    page.on('console', e => { if (e.type() === 'error') report.errors.push(e.text()); });
    await page.route('**/*', route => { report.externalRequests.push(route.request().url()); return route.abort(); });
    await page.setContent('<!doctype html><html data-ui-style="modern" data-theme="light" data-theme-style="slate"><body><div id="root"></div></body></html>');
    const css = fs.readFileSync(path.join(root, 'src/styles.css'), 'utf8');
    await page.addStyleTag({ content: css });
    await page.addStyleTag({ content: '#root{height:100vh} #fixture-frame{display:flex;flex-direction:column;height:100%;width:100%} #fixture-frame>.main{flex:1;min-height:0} #fixture-frame>.footer{height:100px;flex:0 0 100px}' });
    await page.addScriptTag({ content: bundle.outputFiles[0].text });
    const settle = () => page.waitForTimeout(300);
    const geometry = () => page.evaluate(() => {
      const el = document.querySelector('.transcript');
      const rect = n => { if (!n) return null; const {left,right,top,bottom,width,height}=n.getBoundingClientRect(); return {left,right,top,bottom,width,height}; };
      const top = el.getBoundingClientRect().top;
      const anchor = [...document.querySelectorAll('[data-transcript-anchor]')].find(n => n.getBoundingClientRect().bottom > top + 1);
      return {top:el.scrollTop, gap:el.scrollHeight-el.clientHeight-el.scrollTop, height:el.scrollHeight,
        anchor: anchor && {id:anchor.dataset.transcriptAnchor, top:anchor.getBoundingClientRect().top-top},
        shell:rect(document.querySelector('.transcript-shell')), scroller:rect(el),
        follow:rect(document.querySelector('.transcript-follow')), todo:rect(document.querySelector('.todobar')), popup:rect(document.querySelector('.todo-popover')),
        content:rect(document.querySelector('.transcript-content')), users:document.querySelectorAll('[data-question-anchor]').length,
        overflow: el.scrollWidth-el.clientWidth};
    });
    const reset = async () => { await page.evaluate(items => window.fixture.reset(items), history()); await settle(); };
    const wheel = async delta => { const r = await page.locator('.transcript').boundingBox(); await page.mouse.move(r.x+r.width*.7,r.y+r.height*.5); await page.mouse.wheel(0,delta); };
    const follow = async () => { if (await page.locator('.transcript-follow').count()) await page.locator('.transcript-follow').click(); await settle(); };
    const stream = async (text = 'Additional streamed text. '.repeat(60)) => { await page.evaluate(text => window.fixture.grow(text), text); await settle(); };
    const stationary = (before, after, label) => check(before.anchor.id === after.anchor.id && Math.abs(before.anchor.top-after.anchor.top) <= 1, label, {before,after});
    const shot = async name => { await page.screenshot({ path: path.join(output, name), animations: 'disabled' }); report.screenshots.push(name); };

    await reset();
    check((await geometry()).gap <= 4, 'Initial recent history follows bottom');
    check(await page.locator('.warm-turn').count() === 0, 'No forced older-history cards');
    check((await geometry()).users === 30, 'Initial render bounded to 30 complete turns');
    // Observe an already-rendered question while the real wheel crosses the
    // paging threshold: its only movement should be the requested 200px.
    await wheel(-400); await settle();
    await page.evaluate(() => { document.querySelector('.transcript').scrollTop=420; }); await settle();
    const pageAnchor = await page.locator('#question-anchor-u52').boundingBox();
    await wheel(-200); await settle();
    const prependedAnchor = await page.locator('#question-anchor-u52').boundingBox();
    check((await geometry()).users===50 && Math.abs(prependedAnchor.y-pageAnchor.y-200)<=1,
      'Automatic prepend preserves the existing visible geometry', {before:pageAnchor.y,after:prependedAnchor.y});
    await reset();
    await wheel(-40);
    await page.waitForTimeout(60);
    check((await geometry()).gap >= 35, 'One small tick is not immediately corrected');
    await settle();
    check((await geometry()).gap <= 4, 'One small tick snaps after 180ms');
    await wheel(-120); await settle();
    check((await geometry()).gap <= 4, 'A single 120px tick stays inside the follow tolerance', await geometry());
    await wheel(-140); await settle();
    check((await geometry()).gap >= 135, 'A single tick beyond 128px intentionally detaches');
    await follow();
    await wheel(-40); await page.waitForTimeout(40); await wheel(-40); await settle();
    let before = await geometry();
    check(before.gap >= 70, 'Continued small ticks intentionally detach inside 128px');
    await stream(); stationary(before, await geometry(), 'Streaming respects continued scroll');
    await follow();

    // A pointer click and a real text selection must not detach.
    const live = page.locator('[data-transcript-anchor="stream-text"] .msg__body');
    const box = await live.boundingBox();
    await page.mouse.click(box.x+30, Math.min(box.y+30,700));
    await stream('Click remains following. '.repeat(25));
    check((await geometry()).gap <= 4, 'Click does not detach follow');
    const last = await live.evaluate(el => {
      const top=document.querySelector('.transcript').getBoundingClientRect().top;
      const bottom=document.querySelector('.todobar').getBoundingClientRect().top;
      const walker=document.createTreeWalker(el,NodeFilter.SHOW_TEXT);
      while(walker.nextNode()) {
        const range=document.createRange(); range.selectNodeContents(walker.currentNode);
        for(const r of range.getClientRects()) if(r.top>top+5 && r.bottom<bottom-5 && r.width>100)
          return {x:r.left+4,y:r.top+r.height/2};
      }
      throw new Error('No visible text fragment for selection');
    });
    await page.mouse.move(last.x, last.y); await page.mouse.down();
    await page.mouse.move(last.x+80, last.y, {steps:8}); await page.mouse.up();
    check(await page.evaluate(() => getSelection().toString().length > 0), 'Real selection fixture selected text');
    await stream('Selection remains following. '.repeat(25));
    check((await geometry()).gap <= 4, 'Text selection does not detach follow');
    await page.evaluate(() => getSelection().removeAllRanges());

    for (const key of ['PageUp', 'Home']) {
      await follow(); await page.locator('.transcript').focus(); await page.keyboard.press(key); await settle();
      before = await geometry(); check(before.gap > 128, `${key} intentionally detaches`);
      await stream(); stationary(before, await geometry(), `${key} reading anchor survives streaming`);
    }
    // Home reaches the loaded edge and triggers automatic prepend without a click.
    check((await geometry()).users > 30, 'Reaching the top auto-pages full older turns');
    for (let pages=0; pages<5 && (await geometry()).users < 81; pages++) {
      await page.locator('.transcript').focus(); await page.keyboard.press('Home'); await settle();
    }
    check(await page.locator('#question-anchor-u0').count() === 1, 'Oldest original question available continuously');
    check(await page.locator('.warm-collapse').count() === 0, 'No paging affordance after all originals load');
    check(await page.locator('.timeline-entry--completed').count() === 80, 'Older assistant replies render in full');
    before = await geometry(); await stream(); stationary(before, await geometry(), 'Loaded history stays anchored during live streaming');
    await shot('continuous-history.png');
    // Loading a remote rail target does not reset another turn's open state.
    await reset();
    await page.locator('.turn-process-panel > button').first().click();
    await page.locator('.jump-item').first().focus(); await page.keyboard.press('Enter'); await settle();
    const jump = await page.locator('#question-anchor-u0').boundingBox();
    check(Math.abs(jump.y-(await geometry()).scroller.top-12)<=2, 'Question rail auto-loads the oldest target at the exact anchor', {jump,geometry:await geometry()});
    check(await page.locator('[data-transcript-anchor="completed-u51"] .turn-process-panel > button').getAttribute('aria-expanded')==='true',
      'Prepending history preserves prior process expansion state');

    // Native scrollbar track/drag, not a programmatic substitute.
    await follow();
    let g = await geometry();
    await shot('scrollbar-before.png');
    await page.mouse.move(g.scroller.right-3, g.scroller.bottom-26); await page.mouse.down();
    await page.mouse.move(g.scroller.right-3, g.scroller.top+g.scroller.height*.45, {steps:12}); await page.mouse.up(); await settle();
    before = await geometry(); await shot('scrollbar-after.png'); check(before.gap > 128, 'Native right-edge scrollbar drag detaches', {g,before});
    await stream(); stationary(before, await geometry(), 'Scrollbar reading survives streaming');

    // Force a resize before the browser dispatches the pending scroll event.
    const race = await page.evaluate(() => {
      const el = document.querySelector('.transcript'); el.scrollTop -= 173;
      const expected = el.scrollTop;
      window.fixture.grow('Resize race. '.repeat(100));
      return expected;
    });
    await settle(); check(Math.abs((await geometry()).top-race) <= 1, 'ResizeObserver never corrects pending manual scrolling');
    await follow();
    await wheel(-40); await page.waitForTimeout(40); await stream('Large growth during grace. '.repeat(500));
    check((await geometry()).gap > 128, 'Grace timer cannot cause a huge jump after large stream growth');

    for (const style of ['modern','classic']) for (const method of ['wheel','scrollbar','End']) {
      await page.evaluate(style => document.documentElement.dataset.uiStyle=style, style);
      await reset(); await stream();
      if (method==='scrollbar') {
        const bounds=(await geometry()).scroller;
        const middle=bounds.top+bounds.height*.5;
        await page.mouse.move(bounds.right-3,bounds.bottom-26); await page.mouse.down();
        await page.mouse.move(bounds.right-3,middle,{steps:12}); await page.mouse.up(); await settle();
        check((await geometry()).gap>128, `${style}: scrollbar detaches before natural return`);
        await page.mouse.move(bounds.right-3,middle); await page.mouse.down();
        await page.mouse.move(bounds.right-3,bounds.bottom-5,{steps:12}); await page.mouse.up(); await settle();
      } else {
        await wheel(-360); await settle();
        check((await geometry()).gap>128, `${style}/${method}: reading starts detached`);
        if (method==='End') {
          await page.locator('.transcript').focus(); await page.keyboard.press('End'); await settle();
        } else {
          await wheel(300); await settle();
          before=await geometry();
          check(before.gap>4 && before.gap<128, `${style}: entering 128px zone does not reattach`);
          await stream(); stationary(before,await geometry(),`${style}: near-bottom reading stays detached`);
          await wheel(100000); await settle();
        }
      }
      check((await geometry()).gap<=2, `${style}/${method}: native input reaches actual bottom`);
      await stream();
      check((await geometry()).gap<=2, `${style}/${method}: new streaming content follows after natural return`);
      if (method==='scrollbar') {
        const bounds=(await geometry()).scroller, middle=bounds.top+bounds.height*.5;
        await page.mouse.move(bounds.right-3,bounds.bottom-26); await page.mouse.down();
        await page.mouse.move(bounds.right-3,middle,{steps:12}); await settle();
        await page.mouse.move(bounds.right-3,bounds.bottom-5,{steps:12}); await settle();
        await stream();
        check((await geometry()).gap<=2, `${style}: reaching bottom mid-drag resumes following`);
        await page.mouse.move(bounds.right-3,middle,{steps:12}); await page.mouse.up(); await settle();
        before=await geometry();
        check(before.gap>128, `${style}: reversing the same scrollbar drag detaches again`);
        await stream(); stationary(before,await geometry(),`${style}: reversed scrollbar drag preserves its reading anchor`);
        await wheel(100000); await settle();
      } else if (method==='End') {
        const bounds=(await geometry()).scroller;
        await page.mouse.click(bounds.right-3,bounds.bottom-26);
        await page.locator('.transcript').focus(); await page.keyboard.press('End'); await settle();
        await stream();
        check((await geometry()).gap<=2, `${style}: End at an already-bottom detached thumb resumes following`);
      }
      // Two deliberate tiny upward ticks cancel grace while remaining inside
      // the reattach tolerance. Upward movement must never count as a return.
      await wheel(-1); await page.waitForTimeout(40); await wheel(-1); await settle();
      before=await geometry();
      check(before.gap>0 && before.gap<=4, `${style}/${method}: deliberate micro-upscroll fixture`);
      await stream(); stationary(before,await geometry(),`${style}/${method}: micro-upscroll remains detached during streaming`);
      check((await geometry()).gap>128, `${style}/${method}: streaming never overrides deliberate upscroll`);
      report.measurements.push({scenario:'natural-bottom-reattach',style,method,after:await geometry()});
    }

    for (const style of ['classic','modern']) for (const width of [760,1280,1600]) {
      await page.setViewportSize({width,height:900});
      await page.evaluate(style => document.documentElement.dataset.uiStyle = style, style);
      await reset(); await wheel(-360); await settle();
      g = await geometry(); report.measurements.push({style,width,...g});
      check(Math.abs(g.shell.right-g.scroller.right)<=1, `${style}/${width}: native scrollbar stays on right edge`);
      check(g.overflow<=1, `${style}/${width}: no horizontal overflow`);
      check(g.follow.bottom+8<=g.todo.top, `${style}/${width}: follow button avoids Todo`, g);
      await page.locator('.todobar__trigger').click(); await settle();
      g = await geometry();
      check(g.popup && g.follow.bottom+8<=g.popup.top, `${style}/${width}: follow button avoids expanded Todo`, g);
      await shot(`${style}-${width}-todo.png`);
      await page.locator('.todobar__trigger').click(); await page.mouse.move(5,5); await page.locator('.transcript').focus(); await settle();
      await page.evaluate(() => window.fixture.todo(false)); await settle();
      g = await geometry(); check(Math.abs(g.shell.bottom-g.follow.bottom-18)<=1, `${style}/${width}: Todo dismissal resets follow position`, g);
    }

    // Compare unaffected Classic reading geometry against the original stylesheet.
    const baselineCSS = execFileSync('git',['show','HEAD:desktop/frontend/src/styles.css'],{cwd:root,encoding:'utf8',maxBuffer:3e6});
    await page.evaluate(() => document.documentElement.dataset.uiStyle='classic'); await reset();
    await page.evaluate(() => window.fixture.todo(false)); await settle();
    const readingStyle = () => page.evaluate(() => Object.fromEntries(['.transcript-content','.msg--user .msg__body','.msg--assistant .msg__body'].map(selector => {
      const n=document.querySelector(selector), s=getComputedStyle(n);
      return [selector,Object.fromEntries(['width','maxWidth','fontSize','lineHeight','padding','borderRadius','color','backgroundColor'].map(key=>[key,s[key]]))];
    })));
    const currentStyle = await readingStyle();
    const baselineTag = await page.addStyleTag({content:baselineCSS}); await settle();
    check(JSON.stringify(currentStyle)===JSON.stringify(await readingStyle()), 'Classic content width and message styling equal original CSS');
    await baselineTag.evaluate(n=>n.remove());

    // Capture both actual style modes; the preceding comparison leaves Classic active.
    for (const style of ['modern', 'classic']) {
    await page.setViewportSize({width:1280,height:1100});
    await page.evaluate(style => document.documentElement.dataset.uiStyle=style, style);
    // Guidance and compaction stay outside completed process details, in one turn.
    await page.evaluate(() => window.fixture.hydrate([
      {role:'user',messageId:'original',turnId:'earlier',content:'Original question'},
      {role:'assistant',messageId:'progress',turnId:'earlier',content:'Earlier answer remains visible'},
      {role:'user',messageId:'current',turnId:'one',content:'Continue with the checks'},
      {role:'assistant',turnId:'one',content:'Progress before guidance',messageId:'pre-progress',toolCalls:[{id:'pre-tool',name:'read',arguments:'{}'}]},
      {role:'tool',toolCallId:'pre-tool',content:'Pre-guidance tool output'},
      {role:'compaction',messageId:'divider',turnId:'one',content:'',summary:'Accepted plan: preserve the original conversation.\nNext: verify continuous history and guidance.\nLiteral text: <script>not executable</script>'},
      {role:'steer',messageId:'guide1',turnId:'one',content:'Please continue carefully'},
      {role:'steer',messageId:'guide2',turnId:'one',content:'Please continue carefully'},
      {role:'assistant',turnId:'one',content:'',toolCalls:[{id:'post-tool',name:'bash',arguments:'{}'}]},
      {role:'tool',toolCallId:'post-tool',content:'Post-guidance tool output'},
      {role:'assistant',messageId:'final',turnId:'one',content:'Final response',reasoning:'Process detail',final:true},
      {role:'turn_stats',turnId:'one',content:'',outcome:'success',tokens:117,elapsedMs:5000},
      {role:'compaction',messageId:'legacy',level:'legacy',content:'',summary:'Unavailable originals'},
    ])); await settle();
    check(await page.locator('[data-question-anchor]').count()===2, 'Guidance does not add to the two original logical user turns');
    check(await page.locator('.steer .msg--user').count()===2, 'Identical guidance IDs render as two normal user bubbles');
    check((await page.locator('.steer__body').allTextContents()).every(t=>t==='\u5f15\u5bfc'), 'Guidance label is faint and localized');
    const guidanceGeometry = await page.locator('.steer').first().evaluate(el => {
      const label=el.querySelector('.steer__body'), bubble=el.querySelector('.msg__body');
      return {gap:bubble.getBoundingClientRect().top-label.getBoundingClientRect().bottom,
        size:parseFloat(getComputedStyle(label).fontSize),bodySize:parseFloat(getComputedStyle(bubble).fontSize)};
    });
    check(guidanceGeometry.gap>=0 && guidanceGeometry.gap<=8 && guidanceGeometry.size<guidanceGeometry.bodySize,
      'Guidance label remains small and adjacent to the normal bubble', guidanceGeometry);
    check(await page.locator('.compaction__summary').count()===2 && await page.locator('.completed-turn__details .compaction__summary').count()===0, 'Compaction dividers are top-level');
    check((await page.locator('[data-transcript-anchor="progress-text"]').textContent()).includes('Earlier answer'), 'Compaction does not hide available original history');
    check((await page.locator('[data-transcript-anchor="legacy"]').textContent()).includes('\u672a\u4fdd\u7559\u539f\u6587'), 'Legacy marker explicitly discloses missing originals');
    const scopedGroups=page.locator('[data-transcript-anchor="process-pre-progress"] .timeline-process-group, [data-transcript-anchor="process-post-tool"] .timeline-process-group');
    check(await scopedGroups.count()===2 && (await scopedGroups.locator('.timeline-process-group__head').evaluateAll(nodes=>nodes.every(n=>n.getAttribute('aria-expanded')==='false'))),
      `${style}: completed pre-guidance and post-guidance tools both start collapsed`);
    check(await page.locator('.tool,.process-progress-row').count()===0 && await page.locator('.steer .msg--user').first().isVisible() &&
      (await page.locator('[data-transcript-anchor="final-text"]').textContent()).includes('Final response'), `${style}: collapsing process preserves guidance and final answer`);
    check(await page.locator('.turn-stats-row').count()===1 && (await page.locator('.turn-stats-row').textContent()).includes('117'), `${style}: full-turn time/token totals appear exactly once`);
    await page.evaluate(() => window.fixture.todo(false)); await settle();
    const divider = page.locator('[data-transcript-anchor="divider"] details');
    const summary = divider.locator('summary');
    check(await summary.textContent()==='\u4e0a\u4e0b\u6587\u5df2\u538b\u7f29', `${style}: completed compaction label is localized`);
    check(await divider.getAttribute('open')===null && !await divider.locator('div').isVisible(), `${style}: compaction summary starts collapsed`);
    const styleEvidence = await page.locator('.steer .msg__body').first().evaluate(el => ({
      mode:document.documentElement.dataset.uiStyle, background:getComputedStyle(el).backgroundColor,
      backgroundImage:getComputedStyle(el).backgroundImage, color:getComputedStyle(el).color
    }));
    report.measurements.push({scenario:'guidance-compaction',style,...styleEvidence});
    check(styleEvidence.mode===style, `${style}: screenshot uses the requested UI mode`, styleEvidence);
    await shot(`${style}-guidance-compaction.png`);
    await summary.focus(); await page.keyboard.press('Enter'); await settle();
    check(await divider.locator('div').isVisible() && (await divider.locator('div').textContent()).includes('Accepted plan:'), `${style}: keyboard opens inspectable summary`);
    check(await divider.locator('script').count()===0 && (await divider.locator('div').textContent()).includes('<script>'), `${style}: summary remains escaped original text`);
    check(await divider.locator('.process-card,.timeline-process-group').count()===0, `${style}: summary is not a process box`);
    check(await page.locator('[data-transcript-anchor="original"]').count()===1 &&
      await page.locator('[data-transcript-anchor="progress-text"]').isVisible(), `${style}: inspecting summary preserves original messages`);
    await shot(`${style}-compaction-summary-open.png`);
    await page.evaluate(() => window.fixture.append([{kind:'notice',id:'summary-update',level:'info',text:'Unrelated timeline update'}]));
    check(await divider.getAttribute('open')!==null, `${style}: unrelated timeline updates preserve summary expansion`);
    await summary.focus(); await page.keyboard.press('Space'); await settle();
    check(await divider.getAttribute('open')===null && !await divider.locator('div').isVisible(), `${style}: keyboard closes the summary`);
    const legacy = page.locator('[data-transcript-anchor="legacy"] details');
    await legacy.locator('summary').click();
    check(await legacy.locator('div').isVisible() && (await legacy.locator('summary').textContent()).includes('\u672a\u4fdd\u7559\u539f\u6587'), `${style}: legacy summary stays inspectable without claiming original recovery`);
    await legacy.locator('summary').click();
    await scopedGroups.first().locator('.timeline-process-group__head').click();
    check((await page.locator('.process-progress-row').textContent()).includes('Progress before guidance') && await page.locator('.tool').count()===1,
      `${style}: pre-guidance tools and progress remain inspectable`);
    await page.evaluate(() => window.fixture.append([{kind:'user',id:'next',text:'Next turn'}, {kind:'assistant',id:'next-answer',text:'Next answer',reasoning:'',streaming:false}]));
    check(await scopedGroups.first().locator('.timeline-process-group__head').getAttribute('aria-expanded')==='true' &&
      await scopedGroups.last().locator('.timeline-process-group__head').getAttribute('aria-expanded')==='false', 'Later turns preserve individual prior process expansion');
    }
    const guidanceModes=report.measurements.filter(m=>m.scenario==='guidance-compaction');
    check(guidanceModes.length===2 && guidanceModes[0].background!==guidanceModes[1].background,
      'Modern and Classic evidence has distinct actual bubble styling', guidanceModes);
    await page.evaluate(() => window.fixture.hydrate([
      {role:'compaction',messageId:'pending',pending:true,content:'',summary:'Not yet accepted'},
      {role:'compaction',messageId:'empty-summary',content:'',summary:'   '}
    ])); await settle();
    check((await page.locator('[data-transcript-anchor="pending"]').textContent())==='\u6b63\u5728\u538b\u7f29\u4e0a\u4e0b\u6587', 'Pending compaction uses the requested status label');
    check(await page.locator('.compaction__summary').count()===2 && await page.locator('details.compaction__summary').count()===0,
      'Pending and empty summaries do not advertise an empty disclosure');
    await page.evaluate(() => window.fixture.reset([
      {kind:'user',id:'finish-user',turnId:'finish',text:'Finish this turn'},
      {kind:'tool',id:'finish-pre',turnId:'finish',name:'read',args:'{}',readOnly:true,status:'done'},
      {kind:'steer',id:'finish-guide',turnId:'finish',text:'Keep this guidance visible'},
      {kind:'tool',id:'finish-post',turnId:'finish',name:'bash',args:'{}',readOnly:false,status:'done'}
    ])); await settle();
    const finishingGroups=page.locator('.timeline-process-group__head');
    check(await finishingGroups.count()===2 && await finishingGroups.evaluateAll(nodes=>nodes.every(n=>n.getAttribute('aria-expanded')==='true')),
      'Live pre-tool and post-tool groups remain expanded before completion');
    await page.evaluate(() => window.fixture.append([
      {kind:'assistant',id:'finish-final',turnId:'finish',text:'The final answer remains visible',reasoning:'',streaming:false,final:true},
      {kind:'turn_stats',id:'finish-stats',turnId:'finish',outcome:'success',success:true,elapsedMs:5000,tokens:117}
    ])); await settle();
    check(await finishingGroups.count()===2 && await finishingGroups.evaluateAll(nodes=>nodes.every(n=>n.getAttribute('aria-expanded')==='false')),
      'Completing pre-tool -> guidance -> post-tool -> final collapses both process groups');
    check(await page.locator('.turn-stats-row').count()===1 && await page.locator('.steer .msg--user').isVisible() &&
      await page.locator('[data-transcript-anchor="finish-final-text"] .msg__body').isVisible(),
      'Completion keeps guidance, final answer and exactly one totals row visible');
    await page.evaluate(() => window.fixture.reset([
      {kind:'user',id:'progress-user',turnId:'progress-only',text:'Show progress'},
      {kind:'assistant',id:'progress-only',turnId:'progress-only',text:'Inspectable intermediate progress',reasoning:'',streaming:false},
      {kind:'steer',id:'progress-guide',turnId:'progress-only',text:'Continue'},
      {kind:'assistant',id:'progress-final',turnId:'progress-only',text:'Final answer',reasoning:'',streaming:false,final:true},
      {kind:'turn_stats',id:'progress-stats',turnId:'progress-only',outcome:'success',success:true}
    ])); await settle();
    const progressHead=page.locator('.timeline-process-group__head');
    check(await progressHead.count()===1 && await progressHead.getAttribute('aria-expanded')==='false',
      'Completed progress-only prefix remains inspectable in compact mode');
    await progressHead.click();
    check(await page.locator('.process-progress-row').textContent()==='Inspectable intermediate progress',
      'Compact mode preserves folded progress text even without a tool');
    await page.evaluate(() => window.fixture.hydrate([
      {role:'compaction',messageId:'leading-legacy',level:'legacy',content:'',summary:'Legacy summary'},
      {role:'user',messageId:'after-legacy',content:'Question after legacy marker'}
    ])); await settle();
    check(await page.locator('[data-transcript-anchor="leading-legacy"]').count()===1,
      'A leading legacy marker is never lost before the first user turn');
    check(report.errors.length===0,'No browser errors',report.errors);
    check(report.externalRequests.length===0,'Fixture never contacts app/backend/network',report.externalRequests);
  } finally {
    fs.writeFileSync(path.join(output,'results.json'),JSON.stringify(report,null,2));
    await browser.close();
    console.log(JSON.stringify({assertions:report.assertions.length,passed:report.assertions.filter(a=>a.passed).length,output}));
  }
})().catch(error => { console.error(error); process.exitCode=1; });
