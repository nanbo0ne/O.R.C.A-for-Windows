(() => {
  if (location.origin !== 'https://chat.deepseek.com' || !['/', '/a/chat', '/a/chat/'].includes(location.pathname)) return;
  const attrs = el => Object.fromEntries(['id','class','role','type','aria-label','aria-pressed','aria-checked','placeholder','accept','multiple','disabled','title'].filter(k=>el.hasAttribute(k)).map(k=>[k,el.getAttribute(k)]));
  const report = () => {
    const fields=[...document.querySelectorAll('textarea,[contenteditable="true"]')].map(el=>({tag:el.tagName,attrs:attrs(el),parents:[]}));
    [...document.querySelectorAll('textarea,[contenteditable="true"]')].forEach((el,index)=>{
      let parent=el.parentElement;
      for(let depth=0;parent&&parent!==document.body&&depth<5;depth++,parent=parent.parentElement) {
        fields[index].parents.push({tag:parent.tagName,attrs:attrs(parent),controls:[...parent.querySelectorAll('button,[role="button"],input[type="file"],.ds-icon-button')].slice(0,25).map(control=>({tag:control.tagName,attrs:attrs(control),label:/^(深度思考|深度思考 \(R1\)|联网搜索|搜索|发送|停止|DeepThink|Search|Send|Stop)$/.test(control.textContent.trim())?control.textContent.trim():'',paths:[...control.querySelectorAll('svg path')].slice(0,3).map(p=>p.getAttribute('d'))}))});
      }
    });
    const modes=[...document.querySelectorAll('div,span,button')].filter(el=>el.childElementCount<4 && /^(深度思考(?:\s*\(R1\))?|联网搜索|搜索|快速|专家|DeepThink(?:\s*\(R1\))?|Search|Think)$/.test(el.textContent.trim())).map(el=>({tag:el.tagName,label:el.textContent.trim(),attrs:attrs(el),parent:el.parentElement?attrs(el.parentElement):{},html:el.parentElement?.outerHTML.replace(/>([^<]*)</g,(all,text)=>/^(\s*|深度思考|联网搜索|搜索|快速|专家|DeepThink|Search|Think)$/.test(text.trim())?all:'><').slice(0,5000)}));
    for (const field of fields) for (const parent of field.parents) for (const control of parent.controls) { if(control.attrs.accept) control.attrs.accept=control.attrs.accept.slice(0,300); }
    const toggles=[...document.querySelectorAll('.ds-toggle-button')].map(el=>({attrs:attrs(el),label:el.textContent.trim().slice(0,100)}));
    window.chrome.webview.postMessage(JSON.stringify({kind:'composer_probe',generation:'__ORCA_DOCUMENT_ID__',text:JSON.stringify({fields,modes,toggles,files:[...document.querySelectorAll('input[type="file"]')].map(el=>({...attrs(el),accept:el.accept.slice(0,300)}))})}));
  };
  setTimeout(report,2000);
})();
