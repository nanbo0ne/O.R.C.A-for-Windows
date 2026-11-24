(() => {
  const command = __ORCA_COMMAND__;
  const generation = __ORCA_GENERATION__;
  const fail = code => window.chrome.webview.postMessage(JSON.stringify({ id: command.id, generation, kind: 'error', code }));
  try {
    const adapter = window.__orcaWebBeta;
    if (!adapter || typeof adapter.run !== 'function') { fail('adapter_missing'); return; }
    if (adapter.generation !== generation) { fail('adapter_generation_mismatch'); return; }
    Promise.resolve(adapter.run(command)).catch(() => fail('dispatch_rejected'));
  } catch { fail('dispatch_exception'); }
})();
