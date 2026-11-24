// Offline synthetic transport regression; no profiles or real website requests.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
// The msedge channel is used when it works; ORCA_TEST_BROWSER=chromium falls back to
// the bundled build when the installed Edge refuses to launch in this environment.
const LAUNCH=process.env.ORCA_TEST_BROWSER==='chromium'?{headless:true}:{headless:true,channel:'msedge'};
const source=fs.readFileSync(path.join(__dirname,'raw-response.js'),'utf8');
const pageSource=fs.readFileSync(path.join(__dirname,'page.js'),'utf8').replaceAll('__ORCA_DOCUMENT_ID__','fixture-generation');
const wireCommands=JSON.parse(process.env.ORCA_SYNTHETIC_PAGE_COMMANDS||'{}');
(async()=>{
 const browser=await chromium.launch(LAUNCH);
 try{for(const mode of ['fetch','xhr','xhr-thinking-stream','xhr-thinking-cancel','xhr-stop','xhr-stop-hung','xhr-burst','xhr-rewrite','rewrite','unknown-batch','unknown-op','missing-terminal','truncated','multiple','cancel','generation','manual','wrong-chat','private-error','large-snapshot','bridge-sized','output-limit','invalid-unicode','resumed-status','fragment-missing','fragment-null','fragment-invalid','fragment-tool','fragment-private','fragment-type-missing','fragment-type-null','fragment-type-invalid','fragment-type-empty',...['REQUEST','TEMPLATE_RESPONSE','FILE','TOOL_SEARCH','TOOL_OPEN','TOOL_FIND','TIP','READ_LINK'].map(t=>'fragment-known-'+t)]){
  const context=await browser.newContext();await context.route('**/*',r=>r.fulfill({body:'<!doctype html><body></body>',contentType:'text/html'}));
  const page=await context.newPage();await page.goto('https://chat.deepseek.com/a/chat/s/synthetic-chat');
  await page.evaluate(mode=>{
   window.mode=mode;window.rows=[];window.alive=true;window.httpEnded=false;
   const text='```json\n{"path":"C:\\\\test","value":"a\\\"b","中文":"图片😀"}\n```  \n';
   window.expected=mode==='large-snapshot'?'x'+'😀'.repeat(60000):mode==='bridge-sized'?'\\'.repeat(1200000):mode==='output-limit'?'x'.repeat(2200000):text;
   const frames=[{v:{response:{fragments:[{type:'THINK',content:'seed'}]}}},{p:'response/fragments/-1/content',o:'APPEND',v:' reason'},...Array.from({length:74},()=>({v:' '})),{p:'response/fragments/-1/elapsed_secs',o:'SET',v:2},{p:'response/fragments',v:[{type:'THINK',content:'seed reason'+' '.repeat(74)},{type:'RESPONSE',content:text}]},{p:'response',o:'BATCH',v:[{p:'accumulated_token_usage',v:42},{p:'quasi_status',v:'FINISHED'}]},{p:'response/status',o:'SET',v:'FINISHED'}];
   if(mode==='rewrite')frames.splice(3,0,{p:'response/fragments/-1/content',o:'SET',v:'changed'});
   if(mode==='missing-terminal')frames.pop();
   if(mode==='unknown-batch')frames.splice(-1,0,{p:'response',o:'BATCH',v:[{p:'content',v:'UNAUTHORIZED'}]});
   if(mode==='unknown-op')frames.splice(3,0,{o:'PRIVATE_OP',v:'bad'});
   if(['large-snapshot','bridge-sized','output-limit'].includes(mode))frames[77].v[1].content=expected;
   if(mode==='invalid-unicode')frames[77].v[1].content='bad\ud800';
   if(mode==='resumed-status')frames.push({p:'response/status',o:'SET',v:'WIP'});
   if(mode==='fragment-missing')delete frames[0].v.response.fragments[0].content;
   if(mode==='fragment-null')frames[0].v.response.fragments[0].content=null;
   if(mode==='fragment-invalid')frames[0].v.response.fragments[0].content={private:'SECRET'};
   if(mode==='fragment-tool')frames[0].v.response.fragments[0].type='TOOL_CALL';
   if(mode==='fragment-private')frames[0].v.response.fragments[0].type='SECRET';
   if(mode==='fragment-type-missing')delete frames[0].v.response.fragments[0].type;
   if(mode==='fragment-type-null')frames[0].v.response.fragments[0].type=null;
   if(mode==='fragment-type-invalid')frames[0].v.response.fragments[0].type={private:'SECRET'};
   if(mode==='fragment-type-empty')frames[0].v.response.fragments[0].type='';
   if(mode.startsWith('fragment-known-')){
    const t=mode.slice('fragment-known-'.length);
    if(t==='TEMPLATE_RESPONSE'){
     // Answer-bearing type: the public site lists it in RESPONSE_TYPES. Give it its
     // own single-fragment wire so the prefix guard is not involved.
     frames.splice(0,frames.length,{v:{response:{fragments:[{type:'TEMPLATE_RESPONSE',content:expected}]}}},{p:'response/status',o:'SET',v:'FINISHED'});
    }else{frames[0].v.response.fragments[0].type=t;}
   }
   if(mode==='xhr-burst'){
    frames.splice(0,frames.length,{v:{response:{fragments:[{type:'RESPONSE',content:''}]}}},{p:'response/fragments/-1/content',o:'APPEND',v:'😀'},...Array.from({length:1999},()=>({v:'中文😀 '})),{p:'response/status',o:'SET',v:'FINISHED'});
    window.expected='😀'+'中文😀 '.repeat(1999);
   }
   window.wire='event: ready\ndata: {}\n\nevent: update_session\ndata: {}\n\n'+frames.map(v=>'data: '+JSON.stringify(v)+'\n\n').join('')+'event: close\ndata: {}\n\n';
   if(mode==='truncated')window.wire=wire.slice(0,-1);
   window.fetch=async()=>{if(mode==='private-error')throw Error('raw_private_secret');return new Response(new ReadableStream({start(c){const b=new TextEncoder().encode(wire),step=['large-snapshot','bridge-sized','output-limit'].includes(mode)?4096:1;for(let i=0;i<b.length;i+=step)c.enqueue(b.slice(i,i+step));c.close();}}),{status:200});};
   class FakeXHR extends EventTarget{open(){}send(){this.status=200;this.responseType='';
    if(mode.startsWith('xhr-thinking')){
     const frame=v=>'data: '+JSON.stringify(v)+'\n\n';
     this.responseText=frame({v:{response:{fragments:[{type:'THINK',content:'first thought 中文😀'}]}}});
     this.dispatchEvent(new Event('progress'));
     setTimeout(()=>{this.responseText+=frame({p:'response/fragments/-1/content',o:'APPEND',v:' second thought'});this.dispatchEvent(new Event('progress'));},90);
     setTimeout(()=>{this.responseText+=frame({p:'response/fragments',o:'APPEND',v:[{type:'RESPONSE',content:text}]})+frame({p:'response/status',o:'SET',v:'FINISHED'});this.dispatchEvent(new Event('progress'));window.httpEnded=true;this.dispatchEvent(new Event('loadend'));},220);
     return;
    }
    this.responseText=wire;this.dispatchEvent(new Event('progress'));if(mode==='xhr-rewrite')this.responseText='rewritten';const finish=()=>{window.httpEnded=true;this.dispatchEvent(new Event('loadend'));};if(mode==='xhr-stop-hung')return;if(['xhr-burst','xhr-stop'].includes(mode))setTimeout(finish,200);else finish();}}
   window.XMLHttpRequest=FakeXHR;
  },mode);
  await page.addScriptTag({content:source});
  const result=await page.evaluate(async()=>{
   const started=performance.now();
   const observer=__orcaRawResponse.start({text:' synthetic prompt\n',url:location.href},()=>{if(!alive)throw Error('manual_intervention');},row=>rows.push({...row,beforeHTTP:!httpEnded,lag:performance.now()-started}));
   const payload=JSON.stringify({prompt:'synthetic prompt',chat_session_id:mode==='wrong-chat'?'another-chat':'synthetic-chat'});
   if(mode.startsWith('xhr')){const x=new XMLHttpRequest();x.open('POST','/synthetic');x.send(payload);}
   else {try{await (await fetch('/synthetic',{method:'POST',body:payload})).text();}catch{}}
   if(mode==='multiple')await(await fetch('/synthetic',{method:'POST',body:payload})).text();
   if(mode==='cancel')observer.close();
   if(mode.startsWith('xhr-stop')){observer.beginStop();observer.close();}
   if(mode==='generation'||mode==='manual')alive=false;
   if(mode==='xhr-thinking-cancel'){await new Promise(r=>setTimeout(r,75));observer.close();}
   let earlyComplete=false;try{earlyComplete=observer.complete();}catch{}
   const closedCount=rows.length;
   await new Promise(r=>setTimeout(r,mode==='bridge-sized'?500:300));
   let error='',complete=false;try{complete=observer.complete();}catch(e){error=e.message;}
   let stopped=false,stopDiagnostic;if(mode.startsWith('xhr-stop')){stopped=observer.stopComplete();stopDiagnostic=observer.stopDiagnostic();}
   observer.close(true);return{error,complete,stopped,stopDiagnostic,earlyComplete,firstLag:rows[0]?.lag,count:rows.length,closedCount,beforeHTTP:rows.some(r=>r.beforeHTTP),max:Math.max(0,...rows.map(r=>r.text.length)),wellFormed:rows.every(r=>r.text.isWellFormed()),text:rows.filter(r=>r.kind==='text').map(r=>r.text).join(''),reason:rows.filter(r=>r.kind==='reasoning').map(r=>r.text).join(''),expected};
  });
  if(['fetch','xhr','xhr-burst','large-snapshot','bridge-sized'].includes(mode)){assert.equal(result.complete,true);assert.equal(result.error,'');assert.equal(result.text,result.expected);assert.equal(result.reason,mode==='xhr-burst'?'':'seed reason'+' '.repeat(74));assert.ok(result.max<=16384);assert.equal(result.wellFormed,true);}
  else if(mode==='xhr-thinking-stream'){
   assert.equal(result.complete,true);assert.equal(result.text,result.expected);
   assert.equal(result.reason,'first thought 中文😀 second thought');
   assert.equal(result.beforeHTTP,true);assert.equal(result.wellFormed,true);
   assert.ok(result.count>=3&&result.count<10,'reasoning was buffered to the end or emitted too often');
  }else if(!(mode==='fragment-tool'||mode==='fragment-private'||mode.startsWith('fragment-known-')))assert.ok(result.error||!result.complete,mode+' falsely completed');
  if(mode==='private-error')assert.equal(result.error,'raw_observation_failed');
  const fragmentErrors={'fragment-missing':'raw_fragment_content_missing','fragment-null':'raw_fragment_content_null','fragment-invalid':'raw_fragment_content_invalid'};
  if(fragmentErrors[mode]){assert.equal(result.error,fragmentErrors[mode]);assert.equal(result.count,0);assert.ok(!result.error.includes('SECRET'));}
  // Inert site types are tolerated: they publish nothing of their own, execute
  // nothing, and do not discard the turn. A real RESPONSE fragment later in the
  // same turn still publishes normally, so text length is asserted against the
  // expected answer rather than zero.
  // TEMPLATE_RESPONSE is answer-bearing: the site's own RESPONSE_TYPES is
  // ["RESPONSE","TEMPLATE_RESPONSE"], so it must reach the model as text and must
  // not fail the turn. The remaining site types are inert.
  if(mode==='fragment-known-TEMPLATE_RESPONSE'){
   assert.equal(result.error,'',mode+' must not fail the turn');
   assert.equal(result.text,result.expected,mode+' must publish the answer-bearing template content');
  }else if(mode==='fragment-tool'||mode==='fragment-private'||mode.startsWith('fragment-known-')){
   assert.equal(result.error,'',mode+' must not fail the turn on an inert fragment type');
   assert.ok(!result.text.includes('seed'),mode+' must not publish inert fragment content');
  }
  if(mode.startsWith('fragment-type-')){
   const suffix=mode.replace(/^fragment-type-/,'').toLowerCase();
   assert.equal(result.error,'raw_fragment_type_'+suffix);assert.equal(result.count,0);assert.equal(result.complete,false);
  }
  if(mode==='output-limit')assert.equal(result.error,'raw_size_limit');
  if(mode==='xhr-burst'){assert.ok(result.beforeHTTP);assert.ok(result.firstLag<=100);assert.ok(result.count<20);assert.equal(result.earlyComplete,false);}
  if(mode==='bridge-sized')assert.equal(result.earlyComplete,false);
  if(mode==='cancel'||mode==='xhr-thinking-cancel')assert.equal(result.count,result.closedCount);
  if(mode.startsWith('xhr-stop')){assert.equal(result.stopped,mode==='xhr-stop');assert.equal(result.count,0);assert.equal(result.complete,false);assert.deepEqual(result.stopDiagnostic,{httpDone:mode==='xhr-stop',outputDrained:true});}
  console.log('PASS '+mode);await context.close();
 }
 for(const mode of ['task','reuse-document','wrong-document','navigation-before-send','no-raw','no-finished','stale-generation','cancel-task','cancel-http-hung','manual-task','parse-error-fetch','parse-error-xhr','parse-error-hung','parse-error-wrong-stop','parse-error-navigation',...Object.keys(wireCommands)]){
  const context=await browser.newContext();await context.route('**/*',r=>r.fulfill({body:'<!doctype html><body></body>',contentType:'text/html'}));
  const page=await context.newPage();await page.goto('https://chat.deepseek.com/a/chat/s/synthetic-chat');
  await page.evaluate(mode=>{
   window.events=[];window.sent=0;window.busy=false;window.responses=[];
   window.chrome={webview:{postMessage:raw=>events.push(JSON.parse(raw))}};
   document.body.innerHTML='<textarea></textarea><button aria-label="深度思考" aria-pressed="false">思考</button><button aria-label="智能搜索" aria-pressed="false">搜索</button><button id="send">Send</button>';
   const field=document.querySelector('textarea'),send=document.querySelector('#send');
   window.expected='```json\n{"value":"a\\\"b","path":"C:\\\\dir"}\n``` \n';
   const frames=[{v:{response:{fragments:[{type:'THINK',content:'secret reasoning'}]}}},{p:'response/fragments',o:'SET',v:[{type:'THINK',content:'secret reasoning'},{type:'RESPONSE',content:expected}]},{p:'response/status',o:'SET',v:'FINISHED'}];
   if(mode==='no-finished')frames.pop();
   if(mode.startsWith('parse-error'))frames.splice(1,0,{p:'response/fragments',o:'APPEND',v:[{type:'RESPONSE',content:{private:'SECRET_UNKNOWN'}}]});
   const wire=frames.map(v=>'data: '+JSON.stringify(v)+'\n\n').join('');
   window.fetch=async()=>new Response(['cancel-http-hung','parse-error-hung'].includes(mode)?new ReadableStream({start(c){c.enqueue(new TextEncoder().encode(wire));}}):wire,{status:200});
   if(mode==='parse-error-xhr'){
    class FakeXHR extends EventTarget{open(){}send(){this.status=200;this.responseType='';this.responseText=wire;this.dispatchEvent(new Event('progress'));setTimeout(()=>this.dispatchEvent(new Event('loadend')),500);}}
    window.XMLHttpRequest=FakeXHR;
   }
   window.__orcaSiteAdapter={detect:()=>true,input:()=>field,send:()=>send,stop:()=>null,busy:()=>busy,responses:()=>responses,disabled:()=>false,read:()=>{if(mode.startsWith('wire-opening'))return{text:'DOM_OPENING',reasoning:''};throw Error('DOM_TEXT_MUST_NOT_BE_READ');},observeGeneration:()=>({sawBusy:()=>true,close:()=>{}})};
   send.onclick=()=>{sent++;busy=true;const text=field.value;field.value='';const body=JSON.stringify({prompt:text.trim(),chat_session_id:'synthetic-chat'});if(mode==='parse-error-xhr'){const xhr=new XMLHttpRequest();xhr.open('POST','/synthetic');xhr.send(body);}else void fetch('/synthetic',{method:'POST',body}).then(r=>r.text());const node=document.createElement('div');node.textContent='LOSSY_RENDERED_TEXT';document.body.append(node);responses.push(node);setTimeout(()=>busy=false,100);};
  },mode);
  if(mode!=='no-raw')await page.addScriptTag({content:source});
  await page.addScriptTag({content:pageSource});
  const command=wireCommands[mode]||{id:'task',text:'synthetic prompt\n',thinking:false,search:false,url:'https://chat.deepseek.com/a/chat/s/synthetic-chat'};
  if(mode==='wrong-document')command.url+='-other';
  await page.evaluate(command=>{window.promise=__orcaWebBeta.run(command);},command);
  if(mode==='navigation-before-send')await page.evaluate(()=>history.pushState({},'', '/a/chat/s/synthetic-other'));
  await page.waitForTimeout(400);
  if(mode==='stale-generation')await page.evaluate(()=>__orcaWebBeta.rebind('new-generation'));
  if(mode==='manual-task')await page.mouse.click(5,5);
  if(['cancel-task','no-finished','cancel-http-hung'].includes(mode))await page.evaluate(()=>__orcaWebBeta.stop('task'));
  if(mode.startsWith('parse-error')){
   await page.waitForFunction(()=>events.some(e=>e.kind==='error'&&e.code==='raw_fragment_content_invalid'));
   await page.evaluate(async command=>{await __orcaWebBeta.run({...command,id:'premature-next'});},command);
   if(mode==='parse-error-wrong-stop')await page.evaluate(()=>__orcaWebBeta.stop('not-this-request'));
   if(mode==='parse-error-navigation')await page.evaluate(()=>history.pushState({},'', '/a/chat/s/synthetic-other'));
   await page.evaluate(()=>__orcaWebBeta.stop('task'));
  }
  await page.waitForTimeout(700);
  if(mode==='reuse-document'){
   await page.evaluate(async command=>{await promise;await __orcaWebBeta.run({...command,id:'task-2',text:'verified tool result\n'});await __orcaWebBeta.run(command);},command);
  }
  const result=await page.evaluate(()=>({events,sent,expected}));
  if(mode==='task'||mode.startsWith('wire-')){
   assert.equal(result.sent,1);assert.equal(result.events.filter(e=>e.kind==='done').length,1);
   assert.equal(result.events.filter(e=>e.kind==='text').map(e=>e.text).join(''),mode.startsWith('wire-opening')?'DOM_OPENING':result.expected);
   assert.equal(result.events.filter(e=>e.kind==='reasoning').map(e=>e.text).join(''),mode.startsWith('wire-opening')?'':'secret reasoning');
  }else if(mode==='reuse-document'){
   assert.equal(result.sent,2);assert.equal(result.events.filter(e=>e.kind==='done').length,2);
   for(const id of ['task','task-2'])assert.equal(result.events.filter(e=>e.kind==='text'&&e.id===id).map(e=>e.text).join(''),result.expected);
   assert.ok(result.events.some(e=>e.id==='task'&&e.code==='dispatch_revoked'));
  }else{assert.equal(result.events.filter(e=>e.kind==='done').length,0);if(['no-raw','wrong-document','navigation-before-send'].includes(mode))assert.equal(result.sent,0);}
  if(['wrong-document','navigation-before-send'].includes(mode))assert.ok(result.events.some(e=>e.code==='conversation_mismatch'&&!e.sent));
  if(mode==='cancel-task')assert.ok(result.events.some(e=>e.kind==='stopped'&&e.stop?.httpDone&&e.stop?.taskExited&&e.stop?.outputDrained&&e.stop?.revoked));
  if(mode==='cancel-http-hung'){
   assert.ok(!result.events.some(e=>e.kind==='stopped'));
   const failed=result.events.find(e=>e.kind==='stop_failed');
   assert.ok(failed);assert.deepEqual(failed.stopDiagnostic,{httpDone:false,uiIdle:true,taskExited:true,revoked:true,outputDrained:true});
   assert.equal(failed.stop,undefined);assert.equal(failed.url,undefined);assert.equal(failed.text,undefined);
  }
  if(mode.startsWith('parse-error')){
   assert.equal(result.sent,1,'failed operation must not admit another send');
   assert.ok(result.events.some(e=>e.id==='premature-next'&&e.code==='page_busy'));
   assert.ok(result.events.some(e=>e.id==='task'&&e.code==='raw_fragment_content_invalid'));
   assert.ok(!result.events.some(e=>e.kind==='stopped'&&e.id==='not-this-request'));
   assert.ok(!result.events.some(e=>e.kind==='text'),'invalid response cannot publish executable text');
   assert.ok(!JSON.stringify(result.events).includes('SECRET_UNKNOWN'));
   const stopped=result.events.filter(e=>e.kind==='stopped');
   if(['parse-error-hung','parse-error-navigation'].includes(mode)){
    assert.equal(stopped.length,0);assert.ok(result.events.some(e=>e.kind==='stop_failed'));
   }else{
    assert.equal(stopped.length,1);
    assert.deepEqual(stopped[0].stop,{httpDone:true,uiIdle:true,taskExited:true,revoked:true,outputDrained:true});
    const next=await page.evaluate(async command=>{await __orcaWebBeta.run({...command,id:'after-stop'});await __orcaWebBeta.stop('after-stop');return sent;},command);
    assert.equal(next,2,'confirmed stop releases admission without replaying the old request');
   }
  }
  console.log('PASS page '+mode);await context.close();
 }
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
