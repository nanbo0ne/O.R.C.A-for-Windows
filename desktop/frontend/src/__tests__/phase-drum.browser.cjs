const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { build } = require('esbuild');
const { chromium } = require('playwright');
const root = path.resolve(__dirname, '../..');
const output = path.resolve(root, '../../.tmp/phase-drum-evidence');
const fixture = `import React from 'react'; import {createRoot} from 'react-dom/client'; import {flushSync} from 'react-dom';
import {PhaseDrum} from './src/components/PhaseDrum';
let phase, generation='one', locale='zh', unavailable=false;
const root=createRoot(document.getElementById('root'));
window.fixture=(next,options={})=>{phase=next;generation=options.generation??generation;locale=options.locale??locale;unavailable=!!options.unavailable;
flushSync(()=>root.render(<section className="work-monitor" style={{height:220,width:280}}>
<header className="work-monitor__heading">工作监控</header><div className="work-monitor__content"><pre>{'合成测试输入\\n\\n模型正在处理测试请求。\\n只使用合成数据。'}</pre></div>
<PhaseDrum phase={phase} locale={locale} generation={generation} unavailable={unavailable}/></section>));};window.fixture();`;
(async()=>{
  await fs.mkdir(output,{recursive:true});
  const bundle=await build({stdin:{contents:fixture,loader:'tsx',resolveDir:root},bundle:true,write:false,format:'iife',platform:'browser',tsconfig:path.join(root,'tsconfig.json'),define:{'process.env.NODE_ENV':'"development"'}});
  const css=await fs.readFile(path.join(root,'src/components/work-monitor.css'),'utf8');
  const frameCSS='body{margin:0;background:#fff;font-family:"Segoe UI","Microsoft YaHei",sans-serif;--bg:#f3f5f7;--fg:#303439;--muted:#7a828b;--border:#dfe4e8}#root{padding:46px 160px}.work-monitor{border:1px solid #dfe4e8;border-radius:6px}.work-monitor__content{padding-top:18px}';
  await fs.writeFile(path.join(output,'preview.html'),`<!doctype html><html lang="zh"><meta charset="utf-8"><title>O.R.C.A. Phase Drum - Synthetic Preview</title><style>${css}\n${frameCSS}\n@media(max-width:620px){#root{padding:32px 16px}}</style><div id="root"></div><script>${bundle.outputFiles[0].text.replace(/<\/script/gi,'<\\/script')}<\/script><script>const sequence=['input','wait-first','reasoning','decode','tool','wait-first','reasoning','decode','final','stopped'];let step=0;fixture(sequence[step]);setInterval(()=>{step=(step+1)%sequence.length;fixture(sequence[step])},1600);<\/script></html>`);
  const browser=await chromium.launch({headless:true,executablePath:process.env.ORCA_BROWSER_EXECUTABLE||(process.platform==='win32'?'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe':undefined)});
  const report={assertions:[],errors:[]};
  const check=(value,label,data)=>{report.assertions.push({label,passed:!!value,data});assert.ok(value,label)};
  let context,page,video;
  try{
    context=await browser.newContext({viewport:{width:600,height:340},...(process.env.ORCA_RECORD_VIDEO?{recordVideo:{dir:output,size:{width:600,height:340}}}:{})});
    page=await context.newPage();video=page.video();
    page.on('pageerror',e=>report.errors.push(e.message));
    await page.route('**/*',r=>r.abort());
    await page.setContent('<html><body><div id="root"></div></body></html>');
    await page.addStyleTag({content:css});
    await page.addStyleTag({content:frameCSS});
    await page.addScriptTag({content:bundle.outputFiles[0].text});
    const drum=page.locator('.phase-drum'),surface=page.locator('.phase-drum__window');
    const pos=()=>drum.evaluate(e=>Number(e.dataset.position));
    check(await page.locator('.phase-drum__face').count()===0,'No phase is invented before an event');
    await page.evaluate(()=>fixture('input'));await page.waitForTimeout(50);
    check((await surface.boundingBox()).height===6,'Collapsed colored surface is six pixels');
    check((await page.locator('.phase-drum__frame').boundingBox()).height===12,'External frame wraps collapsed strip with a fixed gap');
    check(await surface.evaluate(e=>getComputedStyle(e,'::after').boxShadow!=='none'&&getComputedStyle(e,'::after').pointerEvents==='none'),'Inset rim is decorative and cannot intercept input');
    check(await page.locator('.phase-drum__face').first().evaluate(e=>getComputedStyle(e).boxShadow!=='none'),'Faces have a subtle separator');
    check(await drum.getAttribute('role')==='img','Readonly graphic, not slider or control');
    await page.screenshot({path:path.join(output,'collapsed.png')});
    await drum.hover();await page.waitForTimeout(250);
    check((await surface.boundingBox()).height===28,'Hover expands to 28px');
    check((await page.locator('.phase-drum__frame').boundingBox()).height===34,'External frame follows expanded height');
    check(await page.locator('.phase-drum__face--0 .phase-drum__label').evaluate(e=>getComputedStyle(e).opacity)==='1','Hover reveals phase label');
    await page.screenshot({path:path.join(output,'expanded-input.png')});
    const labelSamples=await page.evaluate(async()=>{
      const samples=[];fixture('wait-first');
      for(let i=0;i<28;i++){
        await new Promise(requestAnimationFrame);
        const read=sector=>{
          const face=document.querySelector('.phase-drum__face--'+sector);
          const label=face?.querySelector('.phase-drum__label');if(!label)return null;
          const f=face.getBoundingClientRect(),l=label.getBoundingClientRect();
          return {text:label.textContent,x:l.x,centre:l.x+l.width/2,faceCentre:f.x+f.width/2};
        };
        samples.push({old:read(0),next:read(1)});
      }return samples;
    });
    const oldLabels=labelSamples.map(s=>s.old).filter(Boolean);
    const newLabels=labelSamples.map(s=>s.next).filter(Boolean);
    check(oldLabels.every(s=>s.text==='输入'),'Outgoing label keeps its original text');
    check(oldLabels[0].x-oldLabels.at(-1).x>10&&newLabels[0].x-newLabels.at(-1).x>10,'Both labels physically slide with the colors');
    check([...oldLabels,...newLabels].every(s=>Math.abs(s.centre-s.faceCentre)<1),'Text stays attached to its own moving face');
    await page.screenshot({path:path.join(output,'transition-labels.png')});
    await page.evaluate(()=>fixture('input',{generation:'motion-tests'}));await page.waitForTimeout(50);
    for(const [phase,target] of [['wait-first',1],['reasoning',2],['decode',3],['tool',4]]){
      const samples=await page.evaluate(async([phase])=>{
        fixture(phase);const a=[];for(let i=0;i<42;i++){await new Promise(requestAnimationFrame);const d=document.querySelector('.phase-drum');a.push({p:Number(d.dataset.position),faces:document.querySelectorAll('.phase-drum__face').length});}return a;
      },[phase]);
      check(samples.every((s,i)=>s.faces<=3&&(!i||s.p>=samples[i-1].p)),'Forward-only transition with at most three faces: '+phase,samples);
      await page.waitForTimeout(700);
      check(await pos()===target,'Correct centered sector: '+phase,await pos());
      if(phase==='reasoning')await page.screenshot({path:path.join(output,'expanded-reasoning.png')});
    }
    const before=await pos();
    await page.mouse.wheel(240,0);await page.keyboard.press('ArrowRight');
    const b=await drum.boundingBox();await page.mouse.move(b.x+60,b.y+10);await page.mouse.down();await page.mouse.move(b.x+180,b.y+10,{steps:8});await page.mouse.up();
    check(await pos()===before,'Wheel, keyboard and dragging cannot rotate the indicator');
    check(await surface.evaluate(e=>e.scrollWidth===e.clientWidth&&getComputedStyle(e).overflowX==='clip'),'No horizontal scroll surface');
    await page.evaluate(()=>fixture('decode'));await page.waitForTimeout(70);
    await page.evaluate(()=>fixture('paused'));const paused=await pos();await page.waitForTimeout(700);
    check(await pos()===paused,'Pause stops an in-flight animation');
    await page.evaluate(()=>fixture('reasoning'));await page.waitForTimeout(35);
    await page.evaluate(()=>fixture('tool'));await page.waitForTimeout(35);
    await page.evaluate(()=>fixture('decode'));await page.waitForTimeout(700);
    check((await pos())%4===3,'Rapid changes coalesce into latest real phase');
    await page.evaluate(()=>fixture('stopped'));const stopped=await pos();await page.waitForTimeout(700);
    check(await pos()===stopped,'Stopped state does not run an infinite timer animation');
    await page.mouse.move(10,10);await drum.evaluate(e=>e.blur());await page.keyboard.press('Tab');await page.waitForTimeout(250);
    check((await surface.boundingBox()).height===28,'Keyboard focus can inspect without changing phase');
    await drum.evaluate(e=>e.blur());await page.waitForTimeout(250);
    check((await surface.boundingBox()).height===6,'Leaving shrinks the surface');
    await page.emulateMedia({reducedMotion:'reduce'});await page.evaluate(()=>fixture('wait-first'));
    check((await pos())%4===1,'Reduced motion jumps directly to actual phase');
    await page.evaluate(()=>fixture('error',{generation:'new'}));
    check(await page.locator('.phase-drum__face').count()===0,'New generation never inherits old observed colors');
    await page.unroute('**/*');
    await page.route(/^https?:/,r=>r.abort());
    await page.goto(require('node:url').pathToFileURL(path.join(output,'preview.html')).href);
    await page.locator('.phase-drum--observed').waitFor();
    check(await page.locator('.phase-drum').getAttribute('data-phase')==='input','Standalone preview opens without a server');
    await page.waitForTimeout(2300);
    check(await page.locator('.phase-drum').getAttribute('data-phase')==='wait-first','Standalone synthetic preview animates real component');
    check(report.errors.length===0,'No browser errors',report.errors);
  }finally{
    if(context)await context.close();if(video)await video.saveAs(path.join(output,'phase-drum-demo.webm'));
    await browser.close();await fs.writeFile(path.join(output,'results.json'),JSON.stringify(report,null,2));
  }
  console.log(JSON.stringify({assertions:report.assertions.length,failed:report.assertions.filter(x=>!x.passed).length,output}));
})().catch(e=>{console.error(e);process.exitCode=1});
