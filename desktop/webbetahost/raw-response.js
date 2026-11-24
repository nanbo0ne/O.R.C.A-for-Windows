// Observe only the currently owned UI request; never issue a website request.
(() => {
  if (window !== window.top || location.origin !== 'https://chat.deepseek.com' || window.__orcaRawResponse) return;
  let active = false;
  const fail = code => { throw Error(code); };
  const errors=new Set('raw_batch_invalid raw_batch_unknown raw_binding_unavailable raw_busy raw_closed raw_content_unknown raw_duplicate_snapshot raw_event_unknown raw_fragment_missing raw_fragment_unknown raw_fragments_invalid raw_frame_limit raw_http_failed raw_identity_changed raw_invalid_json raw_invalid_unicode raw_line_limit raw_metadata_invalid raw_multiple_matches raw_op_invalid raw_op_unknown raw_patch_invalid raw_path_invalid raw_request_limit raw_response_rewritten raw_size_limit raw_sse_unknown raw_status_unknown raw_stream_truncated raw_xhr_type'.split(' '));
  function start(command, check, emit) {
    if (active) fail('raw_busy');
    const chat = /^\/a\/chat\/s\/([^/?#]+)$/.exec(location.pathname)?.[1];
    if (!chat || command.url !== location.href || typeof command.text !== 'string' || !command.text.trim()) fail('raw_binding_unavailable');
    active = true;
    const fetchOriginal=window.fetch, openOriginal=XMLHttpRequest.prototype.open, sendOriginal=XMLHttpRequest.prototype.send;
    const eligibleXHR=new WeakSet(), listeners=new Map();
    let closed=false,stopping=false,error='',transportInvalid=false,matched=0,httpDone=false,finished=false,reader=null;
    let ownedXHR=null;
    let fragments=[],lastText='',lastReason='',path='',op='',outputBytes=0;
    const pendingRows=[];
    let timer=null;
    const transportGuard=()=>{if(closed)fail('raw_closed');if(!stopping)check();if(location.href!==command.url)fail('raw_identity_changed');if(transportInvalid)fail(error);};
    const guard=()=>{transportGuard();if(error)fail(error);};
    const record=(e,parserFailure=false)=>{
      if(!parserFailure)transportInvalid=true;
      error ||= errors.has(e?.message)?e.message:'raw_observation_failed';
      if(timer!==null)clearTimeout(timer);timer=null;pendingRows.length=0;
    };
    function wellFormed(text) {
      for(let i=0;i<text.length;i++) {
        const code=text.charCodeAt(i);
        if(code>=0xD800&&code<=0xDBFF){const low=text.charCodeAt(++i);if(!(low>=0xDC00&&low<=0xDFFF))fail('raw_invalid_unicode');}
        else if(code>=0xDC00&&code<=0xDFFF)fail('raw_invalid_unicode');
      }
    }
    function drain() {
      timer=null;
      try {
        guard();
        for(let n=0;n<8&&pendingRows.length;n++) {
          const row=pendingRows[0];
          if(row.parts){row.text=row.parts.join('');row.parts=null;row.offset=0;}
          let end=Math.min(row.offset+16384,row.text.length);
          const last=row.text.charCodeAt(end-1);if(last>=0xD800&&last<=0xDBFF)end--;
          guard();emit({kind:row.kind,text:row.text.slice(row.offset,end)});row.offset=end;
          if(end===row.text.length)pendingRows.shift();
        }
      }catch(e){record(e);pendingRows.length=0;}
      if(pendingRows.length&&!closed&&!error)timer=setTimeout(drain,20);
    }
    function chunks(kind,text) {
      if(!text)return;
      const previous=pendingRows[pendingRows.length-1];
      if(previous?.kind===kind&&previous.parts)previous.parts.push(text);
      else pendingRows.push({kind,parts:[text]});
      if(timer===null)timer=setTimeout(drain,50);
    }
    function publish(kind,delta) {
      wellFormed(delta);outputBytes+=new TextEncoder().encode(delta).length;
      if(outputBytes>2<<20)fail('raw_size_limit');
      chunks(kind,delta);
      if(kind==='text')lastText+=delta;else lastReason+=delta;
    }
    // Website-defined answer-bearing types. RESPONSE_TYPES in the public site
    // bundle is ["RESPONSE","TEMPLATE_RESPONSE"]; MAIN_RESPONSE_TYPES only
    // excludes the template variant from some UI grouping rules.
    const ANSWER_TYPES=new Set(['RESPONSE','TEMPLATE_RESPONSE']);
    // Types the site emits that are never agent text. They are recorded as
    // inert: no content is published, nothing becomes executable, and the turn
    // is not discarded just because the site used one.
    const INERT_TYPES={SEARCH:'search',TOOL_SEARCH:'tool_search',TOOL_OPEN:'tool_open',TOOL_FIND:'tool_find',TIP:'tip',REQUEST:'request',READ_LINK:'read_link',FILE:'file',TOOL:'tool',TOOL_CALL:'tool_call',TOOL_RESULT:'tool_result',ANSWER:'answer'};
    function flush() {
      // The site streams either RESPONSE or TEMPLATE_RESPONSE for one turn, not
      // both. Preferring RESPONSE keeps the rewrite guard honest when the site
      // switches from a template placeholder to the real answer mid-turn.
      const pick=fragments.some(f=>f.type==='RESPONSE')?'RESPONSE':'TEMPLATE_RESPONSE';
      const text=fragments.filter(f=>f.type===pick&&typeof f.content==='string').map(f=>f.content).join('');
      const reason=fragments.filter(f=>f.type==='THINK'&&typeof f.content==='string').map(f=>f.content).join('');
      if (!text.startsWith(lastText)||!reason.startsWith(lastReason))fail('raw_response_rewritten');
      const textDelta=text.slice(lastText.length),reasonDelta=reason.slice(lastReason.length);
      publish('reasoning',reasonDelta);publish('text',textDelta);
    }
    function fragmentFailure(code) {
      errors.add(code);fail(code);
    }
    function snapshot(value) {
      if(!Array.isArray(value)||value.length>256)fail('raw_fragments_invalid');
      return value.map(f=>{
        if(!f||typeof f!=='object'||Array.isArray(f))fragmentFailure('raw_fragment_invalid');
        if(!Object.hasOwn(f,'type'))fragmentFailure('raw_fragment_type_missing');
        if(f.type===null)fragmentFailure('raw_fragment_type_null');
        if(typeof f.type!=='string')fragmentFailure('raw_fragment_type_invalid');
        if(!f.type)fragmentFailure('raw_fragment_type_empty');
        const isAnswer=ANSWER_TYPES.has(f.type),isThink=f.type==='THINK';
        if(!isAnswer&&!isThink) {
          // Diagnostic label only. The label is a fixed enum so no page-supplied
          // type string or content can reach logs. Inert types are tolerated:
          // failing the whole turn on a SEARCH/TIP/FILE fragment made normal
          // site responses unusable, since the site emits them routinely.
          const label=Object.hasOwn(INERT_TYPES,f.type)?INERT_TYPES[f.type]:'unknown';
          errors.add('raw_fragment_type_'+label);
          return{type:f.type,content:'',inert:true};
        }
        if(!Object.hasOwn(f,'content'))fragmentFailure('raw_fragment_content_missing');
        if(f.content===null)fragmentFailure('raw_fragment_content_null');
        if(typeof f.content!=='string')fragmentFailure('raw_fragment_content_invalid');
        return{type:f.type,content:f.content};
      });
    }
    function metadata(p,v) {
      if(['response/accumulated_token_usage','response/fragments/-1/elapsed_secs'].includes(p)||/^response\/fragments\/(?:-?[0-9]+)\/elapsed_secs$/.test(p)) {
        if(typeof v!=='number'||!Number.isFinite(v)||v<0)fail('raw_metadata_invalid');return true;
      }
      if(['response/status','response/quasi_status'].includes(p)) {
        // Public site status enum: FINISHED(=OK)/WIP/INCOMPLETE/CONTENT_FILTER/
        // CONTEXT_LENGTH_EXCEEDED/TIMEOUT, plus the streaming aliases below.
        // Only progress states continue the turn; the rest end it with a code
        // that names the real cause instead of a generic unknown-status error.
        const progress=['WIP','THINKING','RUNNING'];
        const done=v==='FINISHED'||v==='OK';
        if(p==='response/status')finished=done;
        // quasi_status carries the same terminal value before response/status
        // does, so accept it here without letting it set the stop flag.
        if(progress.includes(v)||done)return true;
        const terminal={INCOMPLETE:'raw_status_incomplete',CONTENT_FILTER:'raw_status_content_filter',CONTEXT_LENGTH_EXCEEDED:'raw_status_context_length',TIMEOUT:'raw_status_timeout'};
        fail(Object.hasOwn(terminal,v)?terminal[v]:'raw_status_unknown');
        return true;
      }
      return false;
    }
    function patch(value) {
      if(!value||typeof value!=='object'||Array.isArray(value))fail('raw_patch_invalid');
      if(value.v?.response) {
        if(fragments.length||path||op)fail('raw_duplicate_snapshot');
        fragments=snapshot(value.v.response.fragments);flush();return;
      }
      if(typeof value.p==='string')path=value.p.replace(/^\//,'');else if('p'in value)fail('raw_path_invalid');
      if(typeof value.o==='string')op=value.o;else if('o'in value)fail('raw_op_invalid');
      if(!['APPEND','SET','BATCH'].includes(op))fail('raw_op_unknown');
      if(path==='response'&&op==='BATCH') {
        if(!Array.isArray(value.v)||value.v.length>32)fail('raw_batch_invalid');
        for(const child of value.v) {
          if(!child||typeof child.p!=='string'||('o'in child&&child.o!=='SET')||!metadata('response/'+child.p,child.v))fail('raw_batch_unknown');
        }
        return;
      }
      if(op==='SET'&&metadata(path,value.v))return;
      if(path==='response/fragments') {
        const list=snapshot(value.v);
        if(op==='SET')fragments=list;else if(op==='APPEND')fragments.push(...list);else fail('raw_op_unknown');
        flush();return;
      }
      const target=/^response\/fragments\/(-?[0-9]+)\/content$/.exec(path);
      if(!target||typeof value.v!=='string'||!['APPEND','SET'].includes(op))fail('raw_content_unknown');
      let index=Number(target[1]);if(index<0)index=fragments.length+index;
      if(!Number.isSafeInteger(index)||!fragments[index])fail('raw_fragment_missing');
      if(op==='APPEND') {
        const fragment=fragments[index];
        if(fragment.inert)return;
        if(fragments.slice(index+1).some(f=>f.type===fragment.type&&f.content))fail('raw_response_rewritten');
        fragment.content+=value.v;publish(fragment.type==='THINK'?'reasoning':'text',value.v);
      } else {if(fragments[index].inert)return;fragments[index].content=value.v;flush();}
    }
    function framing() {
      let pending='',data=[],event='message',bytes=0,frameSize=0;
      const line=s=>{
        frameSize+=s.length;if(frameSize>4<<20)fail('raw_frame_limit');
        if(s==='') {
          const name=event,raw=data.join('\n');event='message';data=[];frameSize=0;
          if(!raw||raw==='[DONE]')return;
          let value;try{value=JSON.parse(raw);}catch{fail('raw_invalid_json');}
          if(['ready','update_session','finish','title','close'].includes(name))return;
          if(name!=='message')fail('raw_event_unknown');
          patch(value);return;
        }
        if(s.startsWith(':'))return;
        const colon=s.indexOf(':'),key=colon<0?s:s.slice(0,colon);let v=colon<0?'':s.slice(colon+1);if(v.startsWith(' '))v=v.slice(1);
        if(key==='data')data.push(v);else if(key==='event')event=v;else if(!['id','retry'].includes(key))fail('raw_sse_unknown');
      };
      return(text,final=false)=>{
        bytes+=text.length;if(bytes>8<<20)fail('raw_size_limit');pending+=text;
        let begin=0;
        for(let i=0;i<pending.length;i++) {
          if(!['\r','\n'].includes(pending[i]))continue;
          if(pending[i]==='\r'&&i===pending.length-1&&!final)break;
          line(pending.slice(begin,i));if(pending[i]==='\r'&&pending[i+1]==='\n')i++;begin=i+1;
        }
        pending=pending.slice(begin);if(pending.length>4<<20)fail('raw_line_limit');
        if(final&&(pending||data.length))fail('raw_stream_truncated');
      };
    }
    const candidate=(url,method)=>{try{return new URL(url,location.href).origin===location.origin&&String(method).toUpperCase()==='POST';}catch{return false;}};
    function bind(raw) {
      if(typeof raw!=='string'||raw.length>2<<20)return false;
      let b;try{b=JSON.parse(raw);}catch{return false;}
      if(!b||Array.isArray(b)||typeof b.prompt!=='string'||b.chat_session_id!==chat)return false;
      if(b.prompt!==command.text&&b.prompt!==command.text.trim())return false;
      let prompts=0,chats=0,visited=0;
      const walk=(v,depth=0)=>{if(++visited>4096||depth>8)fail('raw_request_limit');if(typeof v==='string'){prompts+=v===command.text||v===command.text.trim();chats+=v===chat;}else if(v&&typeof v==='object')for(const child of Object.values(v))walk(child,depth+1);};
      walk(b);if(prompts!==1||chats!==1||++matched!==1)fail('raw_multiple_matches');return true;
    }
    async function observe(response) {
      guard();if(response.redirected||(response.url&&new URL(response.url).origin!==location.origin)||!response.ok||!response.body)fail('raw_http_failed');
      const owned=response.clone().body.getReader(),decode=new TextDecoder('utf-8',{fatal:true}),push=framing();reader=owned;
      let eof=false;
      // A parser failure revokes output, not the terminal transport receipt.
      // Drain this exact response without parsing more data so stop can prove EOF.
      try{while(true){
        transportGuard();const chunk=await owned.read();transportGuard();
        if(chunk.done){
          if(!stopping&&!error){try{push(decode.decode(),true);}catch(e){record(e,true);}}
          eof=true;httpDone=true;break;
        }
        if(!stopping&&!error){try{push(decode.decode(chunk.value,{stream:true}));}catch(e){record(e,true);}}
      }}
      catch(e){if(stopping&&e instanceof DOMException&&e.name==='AbortError')httpDone=true;else throw e;}
      finally{if(!eof)void owned.cancel().catch(()=>{});owned.releaseLock();if(reader===owned)reader=null;}
    }
    const wrappedFetch=function(...args) {
      const request=args[0],options=args[1]||{};let owned=false;
      try{if(candidate(request instanceof Request?request.url:String(request),options.method||(request instanceof Request?request.method:'GET'))){guard();owned=bind(options.body);}}catch(e){record(e);}
      const result=fetchOriginal.apply(this,args);
      if(owned)void result.then(observe).catch(e=>{if(stopping&&e instanceof DOMException&&e.name==='AbortError')httpDone=true;else record(e);});
      return result;
    };
    const wrappedOpen=function(...args){
      // open() may abort/reuse the same native object. Drop the old observer
      // before open can emit its loadend, never credit a replacement request.
      if(ownedXHR===this){ownedXHR=null;listeners.get(this)?.();record(Error('raw_identity_changed'));}
      eligibleXHR.delete(this);if(candidate(String(args[1]),args[0]))eligibleXHR.add(this);return openOriginal.apply(this,args);
    };
    const wrappedSend=function(...args){
      try{if(eligibleXHR.has(this)){guard();if(bind(args[0])){
        const xhr=this,push=framing();ownedXHR=xhr;let previous='';
        const progress=()=>{if(stopping)return;try{transportGuard();if(error)return;if(xhr.responseType&&xhr.responseType!=='text')fail('raw_xhr_type');if(xhr.status<200||xhr.status>=300||(xhr.responseURL&&new URL(xhr.responseURL).origin!==location.origin))fail('raw_http_failed');const next=xhr.responseText;if(!next.startsWith(previous))fail('raw_response_rewritten');const delta=next.slice(previous.length);previous=next;try{push(delta);}catch(e){record(e,true);}}catch(e){record(e);}};
        // A cached native open() can bypass wrappedOpen. Returning to OPENED
        // invalidates the old generation; this is rejection, never stop proof.
        const changed=()=>{if(xhr.readyState===1){if(ownedXHR===xhr)ownedXHR=null;record(Error('raw_identity_changed'));detach();}};
        const finish=()=>{try{
          transportGuard();
          if(!stopping&&!error){progress();if(!error){try{push('',true);}catch(e){record(e,true);}}}
          httpDone=true;
        }catch(e){record(e);}finally{detach();}};
        const detach=()=>{xhr.removeEventListener('readystatechange',changed);xhr.removeEventListener('progress',progress);xhr.removeEventListener('loadend',finish);listeners.delete(xhr);};
        listeners.set(xhr,detach);xhr.addEventListener('readystatechange',changed);xhr.addEventListener('progress',progress);xhr.addEventListener('loadend',finish,{once:true});
      }}}catch(e){record(e);}return sendOriginal.apply(this,args);
    };
    window.fetch=wrappedFetch;XMLHttpRequest.prototype.open=wrappedOpen;XMLHttpRequest.prototype.send=wrappedSend;
    return {
      beginStop(){stopping=true;if(timer!==null)clearTimeout(timer);timer=null;pendingRows.length=0;fragments=[];lastText='';lastReason='';},
      stopComplete(){transportGuard();return stopping&&matched===1&&httpDone&&!pendingRows.length&&timer===null;},
      stopDiagnostic(){return {httpDone:matched===1&&httpDone,outputDrained:stopping&&!pendingRows.length&&timer===null&&fragments.length===0};},
      complete(){guard();return !stopping&&matched===1&&httpDone&&finished&&!!lastText&&!pendingRows.length&&timer===null;},
      close(force=false){if(closed||(stopping&&!force))return;closed=true;ownedXHR=null;if(timer!==null)clearTimeout(timer);timer=null;pendingRows.length=0;if(reader)void reader.cancel().catch(()=>{});if(window.fetch===wrappedFetch)window.fetch=fetchOriginal;if(XMLHttpRequest.prototype.open===wrappedOpen)XMLHttpRequest.prototype.open=openOriginal;if(XMLHttpRequest.prototype.send===wrappedSend)XMLHttpRequest.prototype.send=sendOriginal;for(const detach of [...listeners.values()])detach();fragments=[];lastText='';lastReason='';active=false;}
    };
  }
  Object.defineProperty(window,'__orcaRawResponse',{value:{start},configurable:false});
})();
