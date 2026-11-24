// Offline synthetic native lifecycle; routes never reach an upstream service.
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const source=fs.readFileSync(path.join(__dirname,'raw-response.js'),'utf8');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:'msedge'});
 try{for(const mode of ['normal','hung','wrapped-different','wrapped-same','cached-different','cached-same','cached-after-stop-different','cached-after-stop-same']){
  const context=await browser.newContext();await context.route('**/*',r=>r.fulfill({body:'<!doctype html><body></body>',contentType:'text/html'}));
  const page=await context.newPage();await page.goto('https://chat.deepseek.com/a/chat/s/synthetic');
  await page.evaluate(mode=>{
   window.mode=mode;window.aborts=[];
   class X extends EventTarget{
    open(method,url){this.target=url;this.readyState=1;this.dispatchEvent(new Event('readystatechange'));}
    send(body){this.body=body;this.status=200;this.responseType='';this.readyState=3;this.responseText='data: '+JSON.stringify({v:{response:{fragments:[{type:'THINK',content:'synthetic'}]}}})+'\n\n';this.dispatchEvent(new Event('readystatechange'));this.dispatchEvent(new Event('progress'));}
    abort(){aborts.push(this.target);}
    finish(){this.readyState=4;this.dispatchEvent(new Event('readystatechange'));this.dispatchEvent(new Event('loadend'));}
   }
   window.XMLHttpRequest=X;window.originalOpen=X.prototype.open;window.originalSend=X.prototype.send;
  },mode);
  await page.addScriptTag({content:source});
  const r=await page.evaluate(()=>{
   const observer=__orcaRawResponse.start({id:'owned',url:location.href,text:'synthetic prompt'},()=>{},()=>{});
   const x=new XMLHttpRequest();x.open('POST','/synthetic');x.send(JSON.stringify({prompt:'synthetic prompt',chat_session_id:'synthetic'}));
   const upload=new XMLHttpRequest();upload.open('POST','/unrelated-upload');upload.send(new FormData());
   if(mode.includes('after-stop'))observer.beginStop();
   if(mode.startsWith('wrapped')){x.open('POST',mode.endsWith('same')?'/synthetic':'/unrelated-upload');x.send(new FormData());}
   if(mode.startsWith('cached')){originalOpen.call(x,'POST',mode.endsWith('same')?'/synthetic':'/unrelated-upload');originalSend.call(x,new FormData());}
   observer.beginStop();
   if(mode!=='hung')x.finish();
   upload.finish();
   let stopped=false,error='';try{stopped=observer.stopComplete();}catch(e){error=e.message;}
   const abortAPI=typeof observer.abortOwned;
   observer.close(true);return{stopped,error,aborts,abortAPI};
  });
  assert.deepEqual(r.aborts,[],mode);
  assert.equal(r.abortAPI,'undefined',mode);
  assert.equal(r.stopped,mode==='normal',mode);
  if(mode.includes('cached')||mode.includes('wrapped'))assert.equal(r.error,'raw_identity_changed',mode);
  console.log('PASS XHR-stop-identity '+mode);await context.close();
 }
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
