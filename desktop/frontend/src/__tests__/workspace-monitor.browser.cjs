// Full application, synthetic bindings only. Does not connect to native Wails.
const path = require('node:path');
const fs = require('node:fs/promises');
const qa = require('./transcript-layout.browser.cjs');

function install(mock, locale) {
  const state = window.__monitorFixture ||= { generation: '', seq: 0, entries: [], subscribed: 0, unsubscribed: 0, polls: 0 };
  const settings = mock.Settings.bind(mock);
  mock.Settings = async () => {
    const result = await settings();
    Object.defineProperty(result, 'desktopLanguage', { enumerable: true, get: () => locale, set() {} });
    return result;
  };
  mock.MonitorSubscribe = async tabId => {
    state.generation = 'fixture-' + (++state.subscribed);
    state.tabId = tabId; state.entries = []; state.seq = 0;
    const phases = ['input', 'wait-first', 'reasoning', 'decode', 'tool', 'wait', 'final', 'stopped'];
    phases.forEach((phase, i) => state.entries.push({generation:state.generation, tabId, turnId:'synthetic-turn', seq:++state.seq,
      time:Date.now() - (phases.length-i)*1000, kind:'phase', phase}));
    state.entries.push({generation:state.generation,tabId,turnId:'synthetic-turn',requestId:'request-1',seq:++state.seq,
      time:Date.now(),kind:'request',data:{providerRequestId:'request-1',body:{model:'synthetic-model',stream:true,
        messages:[{role:'user',content:'Synthetic user input: inspect this example.'}],tools:[]}}});
    state.entries.push({generation:state.generation,tabId,turnId:'synthetic-turn',requestId:'request-1',seq:++state.seq,
      time:Date.now(),kind:'response',data:{channel:'decode',text:'Synthetic visible output. '.repeat(20)}});
    return {generation:state.generation,entries:[],cursor:0,evicted:0,dropped:0,bytes:0,expired:false};
  };
  mock.MonitorSnapshot = async (generation,after) => {
    state.polls++;
    return {generation,entries:state.entries.filter(e=>e.seq>after),cursor:state.seq,evicted:0,dropped:0,bytes:2000,expired:false};
  };
  mock.MonitorUnsubscribe = async generation => { if(generation===state.generation) state.unsubscribed++; };
  mock.ContextPanel = async () => ({usedTokens:1200,windowTokens:10000,windowConfirmed:true,totalTokens:96000,
    sessionPromptTokens:90000,sessionCompletionTokens:6000,sessionReasoningTokens:4000,sessionReasoningTokensAvailable:true,
    lastRequestAvailable:true,lastRequestTotalTokens:1400,promptTokens:1000,completionTokens:400,reasoningTokens:300,
    reasoningTokensAvailable:true,requestCount:20,compactRatio:.8,readFiles:[],changedFiles:[]});
  mock.ContextUsage = async () => ({used:1200,window:10000,windowConfirmed:true,sessionTokens:96000,requestCount:20});
}

function fixtureBrowser(browser, locale) {
  return { async newContext(options) {
    const context=await browser.newContext({...options,serviceWorkers:'block'});
    return new Proxy(context,{get(target,key){
      if(key==='route') return (pattern,handler,opts)=>target.route(pattern,route=>handler(new Proxy(route,{get(original,method){
        if(method==='fulfill' && new URL(route.request().url()).pathname==='/src/lib/bridge.ts') return response=>original.fulfill({...response,
          body:response.body+`\n(${install.toString()})(__regressionMock,${JSON.stringify(locale)});\n`});
        const v=original[method];return typeof v==='function'?v.bind(original):v;
      }})),opts);
      const v=target[key];return typeof v==='function'?v.bind(target):v;
    }});
  }};
}

const history=[];
for(let i=0;i<7;i++) {
  history.push({role:'user',turnId:`t${i}`,messageId:`u${i}`,content:`Synthetic question ${i}: verify the visible history.`});
  history.push({role:'assistant',turnId:`t${i}`,messageId:`a${i}`,final:true,content:`Answer ${i}. `+'Readable synthetic content. '.repeat(20)});
  if(i===2)history.push({role:'compaction',messageId:'compact',summary:'Synthetic checkpoint. The original transcript remains above.',trigger:'manual',messages:4});
  if(i===5)history.push({role:'steer',turnId:`t${i}`,messageId:'guide',content:'Preserve the original files.'});
  history.push({role:'turn_stats',turnId:`t${i}`,outcome:'success',tokens:300,elapsedMs:500,finalMessageId:`a${i}`});
}

(async()=>{
  const {url}=qa.configuration();
  const output=path.resolve(process.argv[3]||path.join(__dirname,'../../../../.tmp/next-ui/workspace'));
  await fs.mkdir(output,{recursive:true});
  const report=qa.newReport('workspace-monitor-integration');
  const browser=await qa.chromium.launch({headless:true,executablePath:qa.executablePath,ignoreDefaultArgs:['--hide-scrollbars']});
  try {
    for(const style of ['modern','classic']) for(const width of [760,1024,1366,1920]) {
      const locale=width===1920?'en':'zh';
      const {page,context}=await qa.appPage(fixtureBrowser(browser,locale),report,{url,style,history,width:width<1120?1366:width});
      await qa.panels(page,style,true,false);
      const button=page.locator('.sidebar__navitem').filter({hasText:/工作监控|Work monitor/i});
      await button.click();
      await page.locator('.work-monitor').waitFor();
      await page.waitForFunction(()=>window.__monitorFixture?.polls>0);
      await qa.settle(page);
      if(width<1120) {
        await page.setViewportSize({width,height:900});
        await page.locator('.work-monitor').waitFor({state:'detached'});
        await page.waitForFunction(()=>window.__monitorFixture.unsubscribed>0);
        const polls=await page.evaluate(()=>window.__monitorFixture.polls);
        await page.waitForTimeout(600);
        qa.check(report,await page.evaluate(()=>window.__monitorFixture.polls)===polls,'Narrow hidden sidebar stops capture',{style,width});
        qa.check(report,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'Narrow view has no horizontal overflow',{style,width});
        await qa.screenshot(page,report,output,`${style}-${width}-collapsed.png`);
        report.cases.push({style,width,locale,monitor:'closed by existing responsive sidebar policy'});
        await context.close();continue;
      }
      const bounds=await page.evaluate(()=>{
        const box=s=>{const e=document.querySelector(s),r=e.getBoundingClientRect();return{x:r.x,y:r.y,width:r.width,height:r.height,bottom:r.bottom,right:r.right}};
        return {pane:box('.work-monitor'),sidebar:box('.sidebar'),nav:box('.sidebar__nav'),tree:box('.sidebar__section'),
          ribbon:box('.phase-drum__window'),statusbar:box('.statusbar'),overflow:document.documentElement.scrollWidth-innerWidth,
          viewport:box('.transcript'),chat:box('.transcript-shell'),colors:[...document.querySelectorAll('.phase-drum__face')].map(e=>getComputedStyle(e).backgroundColor)};
      });
      report.cases.push({style,width,locale,bounds});
      qa.check(report,bounds.pane.bottom<=bounds.nav.y+1,'Monitor is above bottom navigation',{style,width,bounds});
      qa.check(report,bounds.pane.y>=bounds.sidebar.y && bounds.nav.bottom<=Math.min(bounds.sidebar.bottom,bounds.statusbar.y)+1,'Monitor and navigation are fully inside visible sidebar',{style,width,bounds});
      qa.check(report,bounds.pane.height>=280,'Default monitor retains its intended usable height',{style,width,bounds});
      qa.check(report,Math.abs(bounds.pane.width-bounds.tree.width)<=4,'Monitor matches conversation selector width',{style,width,bounds});
      qa.check(report,bounds.tree.height>=70,'Conversation selector keeps usable space',{style,width,bounds});
      qa.check(report,bounds.ribbon.height===6,'Read-only phase strip collapses to 6px',{style,width,bounds});
      qa.check(report,bounds.overflow<=1,'No document horizontal overflow',{style,width,bounds});
      qa.check(report,Math.abs(bounds.viewport.right-bounds.chat.right)<=1,'Chat scrollbar stays at right edge',{style,width,bounds});
      qa.check(report,new Set(bounds.colors).size<=3,'Phase window never exposes more than three colors',{style,width,colors:bounds.colors});
      await qa.screenshot(page,report,output,`${style}-${width}-monitor.png`);
      await page.locator('.work-monitor__heading button').filter({hasNot:page.locator('svg.lucide-info')}).last().click();
      await page.locator('.work-monitor').waitFor({state:'detached'});
      await page.waitForFunction(()=>window.__monitorFixture.unsubscribed>0);
      const polls=await page.evaluate(()=>window.__monitorFixture.polls);
      await page.waitForTimeout(650);
      qa.check(report,await page.evaluate(()=>window.__monitorFixture.polls)===polls,'Closed panel stops observation polling',{style,width});
      if(width===1920) {
        await qa.panels(page,style,true,true);
        await page.locator('.context-panel__breakdown').waitFor();
        await qa.settle(page);
        const latest=await page.locator('.context-panel__breakdown').innerText();
        qa.check(report,latest.includes('1,000') && /\b100\b/.test(latest) && /\b300\b/.test(latest) && latest.includes('1,400'),
          'Latest request separates visible output and reasoning without double counting',{style,latest});
        qa.check(report,!latest.includes('96,000'),'Cumulative usage is not placed in latest request breakdown',{style,latest});
        await qa.screenshot(page,report,output,`${style}-${width}-context-usage.png`);
      }
      await context.close();
    }
  }catch(error){qa.check(report,false,'Integration scenario completed',{error:error.stack});}
  finally{await browser.close();await qa.finish(report,output,'results.json');}
})().catch(error=>{console.error(error);process.exitCode=1});
