(() => {
  if (window !== window.top || location.origin !== 'https://chat.deepseek.com') return;
  if (window.__orcaWebBeta) { window.__orcaWebBeta.rebind('__ORCA_DOCUMENT_ID__'); return; }
  let documentID = '__ORCA_DOCUMENT_ID__';
  const post = (event, generation = documentID) => window.chrome.webview.postMessage(JSON.stringify({...event, generation}));
  const site = window.__orcaSiteAdapter;
  const visible = el => !!el && el.getClientRects().length > 0;
  const unique = selector => { const list = [...document.querySelectorAll(selector)].filter(visible); return list.length === 1 ? list[0] : null; };
  const input = () => site ? site.input() : unique('textarea');
  const button = labels => [...document.querySelectorAll('button,[role="button"],[aria-pressed]')].filter(visible).find(el => labels.includes((el.getAttribute('aria-label') || el.textContent || '').trim()));
  const stopButton = () => site ? site.stop() : button(['停止生成', '停止回答', 'Stop generating', 'Stop response']);
  const sendButton = () => site ? site.send() : button(['发送', '发送消息', 'Send', 'Send message']);
  const restricted = () => {
    // Inspect alerts only, never account history/sidebar content.
    const alerts = [...document.querySelectorAll('[role="alert"],[role="dialog"]')].filter(visible).map(el => el.textContent || '').join('\n');
    if (/禁言|账号.*停用|account.*(suspend|banned)|user is muted/i.test(alerts)) return 'restricted';
    if (/验证码|安全验证|captcha|verify.*human/i.test(alerts)) return 'challenge';
    if (/请求.*频繁|稍后再试|too many requests|rate limit/i.test(alerts)) return 'rate_limited';
    if ([...document.querySelectorAll('input[type="password"],input[autocomplete="username"],input[type="tel"]')].some(visible)) return 'signed_out';
    return '';
  };
  let active = null;
  let retiring = null;
  const releaseRetiring = () => {
    if (!retiring) return;
    clearTimeout(retiring.timer);retiring.raw.close(true);retiring=null;
  };
  let manual = false;
  const revoked = new Set();
  let dispatchClosed = false;
  const revoke = id => { if (revoked.size >= 256) dispatchClosed = true; else revoked.add(id); };
  document.addEventListener('pointerdown', event => { if (event.isTrusted && active) { manual = true; active.raw?.close(); } }, true);
  document.addEventListener('keydown', event => { if (event.isTrusted && active) { manual = true; active.raw?.close(); } }, true);
  // Unknown controls fail closed; the fixture cannot enable an unknown live DOM.
  const adapterVerified = () => site ? site.detect() : document.documentElement.dataset.orcaAdapter === 'conformance-v1';
  const state = () => ({ ready: adapterVerified() && !!input() && !!sendButton() && !restricted() && !manual, busy: site ? site.busy() : !!stopButton(), url: location.href, blocked: restricted() || (manual ? 'manual_intervention' : '') || (!adapterVerified() ? 'adapter_not_verified' : '') });
  setInterval(() => post({ kind: 'state', state: state() }), 500);
  const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
  const check = () => { if (!active || active.cancelled) throw new Error('cancelled'); if (manual) throw new Error('manual_intervention'); const code = restricted(); if (code) throw new Error(code); };
  const toggleState = el => el.getAttribute('aria-pressed') ?? el.getAttribute('aria-checked');
  const toggle = (labels, desired) => {
    const el = button(labels);
    if (!el) throw new Error('mode_control_unavailable');
    const raw = toggleState(el);
    if (raw !== 'true' && raw !== 'false') throw new Error('mode_state_unconfirmed');
    if ((raw === 'true') !== desired) el.click();
    return el;
  };
  async function upload(files) {
    if (!files.length) return;
    if(site?.upload){await site.upload(files,check);return;}
    const controls = [...document.querySelectorAll('input[type="file"]')];
    if (controls.length !== 1) throw new Error('upload_control_unavailable');
    const control = controls[0];
    // A site-specific acknowledgement adapter is required before sending files.
    // Unknown website selectors must never silently drop attachments.
    if (!document.querySelector('[data-orca-upload-status]')) throw new Error('upload_acknowledgement_unavailable');
    const transfer = new DataTransfer();
    for (const file of files) {
      check(); const bytes = Uint8Array.from(atob(file.data), ch => ch.charCodeAt(0));
      transfer.items.add(new File([bytes], file.name, { type: file.mime }));
    }
    control.files = transfer.files;
    control.dispatchEvent(new Event('change', { bubbles: true }));
    const deadline = Date.now() + 60000;
    while (Date.now() < deadline) {
      check();
      const states = [...document.querySelectorAll('[data-orca-upload-status]')];
      if (states.some(el => el.dataset.orcaUploadStatus === 'failed')) throw new Error('attachment_upload_failed');
      if (states.filter(el => el.dataset.orcaUploadStatus === 'ready').length === files.length) return;
      await pause(100);
    }
    throw new Error('attachment_upload_timeout');
  }
  async function run(command) {
    const generation = documentID;
    const report = event => post(event, generation);
    const phase = code => report({ id: command.id, kind: 'phase', code });
    if (dispatchClosed || revoked.has(command.id)) { report({ id: command.id, kind: 'error', code: 'dispatch_revoked' }); return; }
    if (active || retiring) { post({ id: command.id, kind: 'error', code: 'page_busy' }); return; }
    active = { id: command.id, cancelled: false }; manual = false;
    const operation = active;
    let sent = false, generationEvidence = null, raw = null;
    const checkRun = () => {
      if (active !== operation || generation !== documentID) throw new Error('cancelled');
      if (!sent && command.url && location.href !== command.url) throw new Error('conversation_mismatch');
      check();
    };
    try {
      phase('dispatch_ack');
      checkRun();
      if (!state().ready || stopButton()) throw new Error('page_not_ready');
      if(command.action==='probe_upload' && site?.uploadProbe){
        await site.uploadProbe(command.attachments||[],checkRun,data=>report({id:command.id,kind:'owned_debug',text:JSON.stringify({upload:data})}));
        report({id:command.id,kind:'done',url:location.href});return;
      }
      const thinking = toggle(['深度思考', '深度思考 (R1)', 'DeepThink', 'DeepThink (R1)'], command.thinking);
      const search = toggle(['智能搜索', '联网搜索', '搜索', 'Search'], command.search);
      await pause(150); checkRun();
      if (toggleState(thinking) !== String(command.thinking) || toggleState(search) !== String(command.search)) throw new Error('mode_state_unconfirmed');
      phase('modes_checked');
      await upload(command.attachments || []); checkRun();
      phase('uploads_ready');
      const field = input();
      if (field.value.trim()) throw new Error('existing_draft');
      const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set;
      set.call(field, command.text); field.dispatchEvent(new Event('input', { bubbles: true }));
      phase('input_set');
      await pause(100); checkRun();
      const previous = new Set(site ? site.responses() : document.querySelectorAll('[data-message-role="assistant"]'));
      const send = sendButton(); if (!send || send.disabled || site?.disabled(send)) throw new Error('send_unavailable');
      generationEvidence = site?.observeGeneration?.();
      // Openings contain no Agent protocol and keep their existing visible-page
      // channel. Real tasks must preserve the pre-Markdown response bytes/text.
      if(command.opening !== true) {
        if(!window.__orcaRawResponse) throw new Error('raw_adapter_unavailable');
        raw=window.__orcaRawResponse.start({...command,url:location.href},checkRun,event=>report({id:command.id,...event}));
        operation.raw=raw;
      }
      phase('send_attempted');
      sent = true; send.click();
      let lastText = '', lastReasoning = '', sawBusy = false, idleAt = 0, ownedResponse = null;
      let debugAt = 0;
      while (true) {
        await pause(50); checkRun();
        const busy = site ? site.busy() : !!stopButton();
        if (!sawBusy && (busy || generationEvidence?.sawBusy() === true)) { sawBusy = true; phase('generation_observed'); }
        const responses = (site ? site.responses() : [...document.querySelectorAll('[data-message-role="assistant"]')]).filter(el => !previous.has(el));
        if (responses.length > 1) throw new Error('ambiguous_response');
        const response = responses[0];
        if (ownedResponse && response !== ownedResponse) throw new Error('response_identity_changed');
        if (response && !ownedResponse) { ownedResponse = response; phase('response_seen'); }
        if(site?.debug && Date.now()-debugAt>1000) {debugAt=Date.now();report({id:command.id,kind:'owned_debug',text:JSON.stringify({busy,sawBusy,response:!!response,textLength:lastText.length,...site.debug(previous)})});}
        if (response && !raw) {
          const text = site ? site.read(response).text : response.querySelector('[data-content="answer"]')?.textContent || '';
          const reasoning = site ? site.read(response).reasoning : response.querySelector('[data-content="reasoning"]')?.textContent || '';
          if (!text.startsWith(lastText) || !reasoning.startsWith(lastReasoning)) throw new Error('response_rewritten');
          if (text !== lastText || reasoning !== lastReasoning) idleAt = 0;
          if (text.length > lastText.length) report({ id: command.id, kind: 'text', text: text.slice(lastText.length) });
          if (reasoning.length > lastReasoning.length) report({ id: command.id, kind: 'reasoning', text: reasoning.slice(lastReasoning.length) });
          lastText = text; lastReasoning = reasoning;
        }
        const rawComplete=raw?.complete();
        if (!busy && (raw ? rawComplete : lastText) && response?.isConnected && input()?.value === '' && sendButton() && adapterVerified()) {
          idleAt ||= Date.now();
          if (sawBusy && Date.now() - idleAt > 400) { report({ id: command.id, kind: 'done', url: location.href }); return; }
          if (!sawBusy && Date.now() - idleAt > 2000) throw new Error('completion_unconfirmed');
        } else idleAt = 0;
      }
    } catch (error) {
      // Retain only the failed owned request until the host consumes its stop
      // proof. Do not leave a parsing failure indistinguishable from a lost send.
      if (raw && sent && !operation.cancelled) {
        raw.beginStop();
        retiring={id:command.id,generation,raw,timer:setTimeout(()=>raw.close(true),10000)};
      }
      report({ id: command.id, kind: 'error', code: String(error.message || 'page_error'), sent });
    } finally { raw?.close(); generationEvidence?.close(); revoke(command.id); if (active === operation) active = null; }
  }
  async function stop(id) {
    revoke(id);
    const generation = documentID;
    if (active && active.id !== id) return;
    if (retiring && (retiring.id!==id || retiring.generation!==generation)) return;
    const operation=active, observation=operation?.raw || retiring?.raw;
    observation?.beginStop();
    if (active) { active.cancelled = true; active.raw?.close(); }
    let clicked = false;
    for (let i = 0; i < 40; i++) {
      const button = stopButton();
      if (button && !clicked) { button.click(); clicked = true; }
      await pause(100);
      if (generation!==documentID)break;
      if (!stopButton() && sendButton() && (!operation||active!==operation)) {
        let drained=false;try{drained=observation?.stopComplete()===true;}catch{}
        if(observation&&!drained)continue;
        observation?.close(true);
        if (retiring?.id===id) releaseRetiring();
        post({ id, kind: 'stopped',stop:drained?{httpDone:true,uiIdle:true,taskExited:true,revoked:revoked.has(id),outputDrained:true}:undefined }, generation); return;
      }
    }
    if(generation===documentID){
      const transport=observation?.stopDiagnostic?.();
      post({id,kind:'stop_failed',stopDiagnostic:{httpDone:transport?.httpDone===true,uiIdle:!stopButton()&&!!sendButton(),taskExited:!operation||active!==operation,revoked:revoked.has(id),outputDrained:transport?.outputDrained===true}},generation);
    }
    observation?.close(true);
    if (retiring?.id===id) releaseRetiring();
  }
  function recheck(id) {
    if (!active && !(site ? site.busy() : !!stopButton()) && !restricted()) manual = false;
    post({ id, kind: 'rechecked', state: state() });
  }
  function rebind(generation) {
    if (generation === documentID) return;
    releaseRetiring();
    if (active) { revoke(active.id); active.cancelled = true; active.raw?.close(); }
    documentID = generation;
    post({ kind: 'state', state: state() });
  }
  Object.defineProperty(window, '__orcaWebBeta', { value: { run, stop, recheck, rebind, get generation() { return documentID; } }, configurable: false });
  post({ kind: 'state', state: state() });
})();
