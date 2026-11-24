(() => {
  if (location.origin !== 'https://chat.deepseek.com' || window !== window.top) return;
  const input = () => {
    const fields=[...document.querySelectorAll('textarea')].filter(el=>el.placeholder.includes('DeepSeek'));
    return fields.length===1?fields[0]:null;
  };
  const composer = () => {
    let p=input()?.parentElement;
    for(let i=0;p&&p!==document.body&&i<6;i++,p=p.parentElement) {
      if(p.querySelector('input[type="file"]') && p.querySelector('.ds-button--circle[role="button"]')) return p;
    }
    return null;
  };
  const primary = () => {
    const nodes=[...(composer()?.querySelectorAll('.ds-button--circle[role="button"]')||[])];
    return nodes.length===1?nodes[0]:null;
  };
  const isSend = el => !!el?.querySelector('svg path[d^="M8.3125 0.980206"]');
  const send = () => isSend(primary()) ? primary() : null;
  const stop = () => {
    const el=primary();
    // Square stop icon observed on the owned synthetic generation, not a generic icon match.
    return el?.querySelector('svg path[d^="M2 4.88C2 3.68009"]') ? el : null;
  };
  const pending = () => !!primary()?.querySelector('svg path[d^="M34,18 C34,9.163444"]');
  const busy = () => !!stop() || pending();
  const detect = () => !!input() && !!composer() && (!!send() || busy()) && document.querySelectorAll('.ds-toggle-button[aria-pressed],.ds-toggle-button[aria-checked]').length===2;
  const observeGeneration = () => {
    const control = primary();
    let observed = false;
    const busyPath = value => typeof value === 'string' && (value.startsWith('M2 4.88C2 3.68009') || value.startsWith('M34,18 C34,9.163444'));
    const busyNode = node => node.nodeType === 1 && (
      (node.matches('svg path') && busyPath(node.getAttribute('d'))) ||
      [...node.querySelectorAll('svg path')].some(path => busyPath(path.getAttribute('d')))
    );
    // Mutation records retain removed icons even when generation finishes before
    // the polling loop runs. Observe only this send control, never chat content.
    const observer = new MutationObserver(records => {
      observed ||= records.some(record => record.type === 'attributes'
        ? record.target.matches('svg path') && (busyPath(record.oldValue) || busyPath(record.target.getAttribute('d')))
        : [...record.addedNodes, ...record.removedNodes].some(busyNode));
    });
    if (control) observer.observe(control, { childList: true, subtree: true, attributes: true, attributeFilter: ['d'], attributeOldValue: true });
    return { sawBusy: () => observed, close: () => observer.disconnect() };
  };
  const observeUploads = () => {
    const original=window.fetch, records=[],owned=new Map();
    const record=(http,json,path)=>{
      const data=json.data?.biz_data;
      let files=[];
      if(path==='/api/v0/file/upload_file') {if(data?.id&&!owned.has(data.id))owned.set(data.id,owned.size);files=[data];}
      else files=(Array.isArray(data)?data:Array.isArray(data?.files)?data.files:Object.values(data||{}).filter(Array.isArray).flat()).filter(file=>owned.has(file?.id));
      if(http!==200||json.code!==0||json.data?.biz_code!==0){
        const message=String(json.data?.biz_msg||json.msg||'');
        const error=http===401?'signed_out':http===429?'rate_limited':/禁言|user is muted|account.*suspend/i.test(message)?'restricted':/验证码|captcha|安全验证/i.test(message)?'challenge':'upload_service_error';
        records.push({error});return;
      }
      for(const file of files)records.push({key:owned.get(file?.id),http,code:json.code,bizCode:json.data?.biz_code,status:file?.status,errorCode:file?.error_code,name:file?.file_name,size:file?.file_size,isImage:file?.is_image,hasID:typeof file?.id==='string'&&file.id.length>0});
    };
    const relevantURL=value=>{const u=new URL(value,location.href);return u.origin===location.origin&&['/api/v0/file/upload_file','/api/v0/file/fetch_files'].includes(u.pathname)?u.pathname:'';};
    const wrapped=function(...args){
      const value=args[0] instanceof Request?args[0].url:String(args[0]);
      const relevant=relevantURL(value);
      const response=original.apply(this,args);
      if(relevant)response.then(async r=>{
        try {
          const json=await r.clone().json();
          record(r.status,json,relevant);
        } catch {records.push({http:r.status,error:'unrecognized_upload_receipt'});}
      },()=>records.push({error:'upload_network_error'}));
      return response;
    };
    window.fetch=wrapped;
    const originalOpen=XMLHttpRequest.prototype.open,originalSend=XMLHttpRequest.prototype.send;
    const uploads=new WeakMap(),listeners=new Map();
    const open=function(...args){uploads.delete(this);const path=relevantURL(String(args[1]));if(path)uploads.set(this,path);return originalOpen.apply(this,args);};
    const send=function(...args){
      if(uploads.has(this)){
        const finish=()=>{try{record(this.status,this.responseType==='json'?this.response:JSON.parse(this.responseText),uploads.get(this));}catch{records.push({error:'unrecognized_upload_receipt'});}listeners.delete(this);};
        listeners.set(this,finish);this.addEventListener('loadend',finish,{once:true});
      }
      return originalSend.apply(this,args);
    };
    XMLHttpRequest.prototype.open=open;XMLHttpRequest.prototype.send=send;
    return {records,close:()=>{
      if(window.fetch===wrapped)window.fetch=original;
      if(XMLHttpRequest.prototype.open===open)XMLHttpRequest.prototype.open=originalOpen;
      if(XMLHttpRequest.prototype.send===send)XMLHttpRequest.prototype.send=originalSend;
      for(const [xhr,listener]of listeners)xhr.removeEventListener('loadend',listener);
    }};
  };
  const upload = async (files,check) => {
    const controls=[...(composer()?.querySelectorAll('input[type="file"]')||[])];
    if(controls.length!==1)throw new Error('upload_control_unavailable');
    if(composer()?.parentElement?.querySelector('.cd314545'))throw new Error('existing_attachment_draft');
    const observation=observeUploads();
    try {
      const transfer=new DataTransfer();
      const expected=files.map(file=>{
        const bytes=Uint8Array.from(atob(file.data),ch=>ch.charCodeAt(0));
        transfer.items.add(new File([bytes],file.name,{type:file.mime}));
        return {name:file.name,size:bytes.length,isImage:file.mime.startsWith('image/')};
      });
      controls[0].files=transfer.files;
      controls[0].dispatchEvent(new Event('change',{bubbles:true}));
      const deadline=Date.now()+60000;
      while(Date.now()<deadline){
        check();
        const failed=observation.records.find(r=>r.error || r.status==='FAILED' || (r.errorCode!==undefined && r.errorCode!==null && r.errorCode!==0));
        if(failed)throw new Error(failed.error||'attachment_upload_failed');
        const completed=new Map();
        for(const r of observation.records)if(r.status==='SUCCESS'&&r.hasID)completed.set(r.key,r);
        const remaining=[...completed.values()];
        const all=expected.every(file=>{
          const index=remaining.findIndex(r=>r.name===file.name&&r.size===file.size&&r.isImage===file.isImage);
          if(index<0)return false;
          remaining.splice(index,1);return true;
        });
        if(all)return;
        await new Promise(resolve=>setTimeout(resolve,100));
      }
      throw new Error('attachment_upload_timeout');
    } finally {observation.close();}
  };
  const uploadProbe = async (files, check, report) => {
    if (!/^\/(a\/chat\/?)?$/.test(location.pathname) || input()?.value || files.length!==1) throw new Error('upload_probe_not_isolated');
    const root=composer();
    const controls=[...root.querySelectorAll('input[type="file"]')];
    if(controls.length!==1)throw new Error('upload_control_unavailable');
    const file=files[0];
    const observation=observeUploads();
    const uploadStart=performance.now();
    const transfer=new DataTransfer();
    transfer.items.add(new File([Uint8Array.from(atob(file.data),ch=>ch.charCodeAt(0))],file.name,{type:file.mime}));
    controls[0].files=transfer.files;
    controls[0].dispatchEvent(new Event('change',{bubbles:true}));
    try { for(let i=0;i<12;i++){
      check();await new Promise(resolve=>setTimeout(resolve,500));
      const area=composer()?.parentElement;
      report({iteration:i, receipts:observation.records,resources:performance.getEntriesByType('resource').filter(e=>e.startTime>=uploadStart && ['fetch','xmlhttprequest'].includes(e.initiatorType)).map(e=>({origin:new URL(e.name).origin,path:new URL(e.name).pathname,status:e.responseStatus,duration:Math.round(e.duration)})),nodes:[...(area?.querySelectorAll('*')||[])].filter(el=>el.childElementCount===0 && (el.textContent.includes(file.name)||/^(上传中|解析中|上传失败|已上传|已完成|删除|移除|100%)$/.test(el.textContent.trim()))).map(el=>({tag:el.tagName,class:el.className,text:el.textContent.slice(0,120),parents:[el.parentElement?.className,el.parentElement?.parentElement?.className],card:el.parentElement?.parentElement?.parentElement?.outerHTML.slice(0,8000)}))});
    }} finally {observation.close();}
  };
  const debug = previous => ({
    route: /^\/a\/chat\/s\//.test(location.pathname) ? 'chat' : /^\/(a\/chat\/?)?$/.test(location.pathname) ? 'fresh' : 'other',
    viewport: [innerWidth, innerHeight], visibility: document.visibilityState,
    inputPresent: !!input(), inputEmpty: input()?.value === '',
    primaryClass: primary()?.className,
    primaryPaths: [...(primary()?.querySelectorAll('svg path') || [])].map(p => p.getAttribute('d')),
    newMarkdown: [...document.querySelectorAll('[class*="markdown"]')].filter(el => !previous.has(el)).slice(0, 8).map(el => ({tag:el.tagName, class:el.className, length:el.textContent.length, ancestors:[el.parentElement?.className,el.parentElement?.parentElement?.className]})),
    notices: [...document.querySelectorAll('[role="alert"],[role="dialog"],.ds-toast')].map(el => ({
      role: el.getAttribute('role'), class: el.className, visible: el.getClientRects().length > 0,
      network: /网络|network|连接|connection/i.test(el.textContent),
      restriction: /禁言|限制|频繁|captcha|验证码|安全验证|suspended/i.test(el.textContent),
      login: /登录|log.?in|sign.?in/i.test(el.textContent),
    })),
  });
  const responses = () => [...document.querySelectorAll('.ds-message')].filter(el=>el.querySelector('.ds-assistant-message-main-content,.ds-think-content'));
  const read = el => ({text:el.querySelector('.ds-assistant-message-main-content')?.innerText||'',reasoning:el.querySelector('.ds-think-content .ds-markdown')?.innerText||''});
  const diagnostics = '__ORCA_DIAGNOSTICS__' === 'enabled';
  window.__orcaSiteAdapter={revision:'deepseek-web-20260925',input,send,stop,busy,detect,observeGeneration,upload,disabled:el=>el?.classList.contains('ds-button--disabled'),responses,read,...(diagnostics?{debug,uploadProbe}:{})};
})();
