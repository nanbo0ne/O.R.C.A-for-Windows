// Real React rendering, synthetic events only; never connects to a model/profile.
const qa = require('./transcript-layout.browser.cjs');
const fs = require('node:fs/promises');

(async () => {
  const {url, output} = qa.configuration();
  await fs.mkdir(output, {recursive:true});
  const report = qa.newReport('web-reasoning-stream');
  const browser = await qa.chromium.launch({headless:true, executablePath:qa.executablePath});
  try {
    for (const style of ['modern','classic']) for (const language of ['zh','en']) for (const width of [760,1024,1366,1920]) {
      const key = `${style}-${language}-${width}`;
      report.cases.push({key});
      const {page,context} = await qa.appPage(browser,report,{url,style,language,width,detailed:true,history:[{role:'user',turnId:'web-live',content:'Synthetic visible thinking test'}]});
      const first='Synthetic visible reasoning / 中文第一段\n';
      const second='Second batch: quoted "value", C:\\synthetic\\file and emoji 😀.';
      await qa.events(page,[{kind:'turn_started',turnId:'web-live'},{kind:'reasoning',turnId:'web-live',messageId:'web-think',itemId:'web-think',text:first}]);
      const body=page.locator('.reasoning__body').last();
      await body.waitFor({state:'visible'});
      qa.check(report,(await body.textContent())===first,'Reasoning visible before any final answer',{key});
      const header=page.locator('.process-card').filter({has:page.locator('.process-card__kind',{hasText:'reasoning'})}).last().locator('button').first();
      await header.click();
      await qa.events(page,[{kind:'reasoning',turnId:'web-live',messageId:'web-think',itemId:'web-think',text:second}]);
      qa.check(report,await page.locator('.reasoning__body:visible').count()===0,'Collapsed reasoning stays hidden while batches arrive',{key});
      await header.click();
      qa.check(report,(await page.locator('.reasoning__body').last().textContent())===first+second,'Reopening preserves exact ordered text without duplicates',{key});
      qa.check(report,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'No horizontal overflow',{key});
      await qa.screenshot(page,report,output,`${key}.png`);
      await context.close();
    }
  } finally {await browser.close();}
  await qa.finish(report,output,'report.json');
})().catch(error=>{console.error(error);process.exitCode=1;});
