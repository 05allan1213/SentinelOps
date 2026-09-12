const { chromium } = require('../../web/node_modules/@playwright/test');
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '../..');
(async () => {
 const auth=JSON.parse(fs.readFileSync(path.join(root,'output/.f-host/auth.json'),'utf8'));
 const browser=await chromium.launch({headless:true});
 const results=[];
 for (const width of [1280,1440]) {
  const facts=JSON.parse(fs.readFileSync(path.join(root,`output/playwright/runtime-${width}/live/facts.json`),'utf8'));
  const context=await browser.newContext({viewport:{width,height:1000}});
  await context.addInitScript(value=>{
   localStorage.setItem('token',value.token);
   localStorage.setItem('auth-storage',JSON.stringify({state:{...value,userID:value.user_id},version:0}));
  },auth);
  const page=await context.newPage();
  const errors=[];
  page.on('console',msg=>{if(msg.type()==='error')errors.push(msg.text())});
  page.on('pageerror',error=>errors.push(error.message));
  const routes=['overview','timeline','attempts','effects','evidence','context','trace'].map(tab=>`/runtime/runs/${facts.run_id}?tab=${tab}`).concat(['/runtime/runs','/runtime/capabilities','/runtime/safety','/runtime/worker-health']);
  for(const route of routes){
   await page.goto('http://127.0.0.1:5174'+route);
   await page.waitForLoadState('networkidle');
   const dimensions=await page.evaluate(()=>({page:document.documentElement.scrollWidth,viewport:document.documentElement.clientWidth,main:document.querySelector('main').scrollWidth,mainWidth:document.querySelector('main').clientWidth}));
   if(dimensions.page!==dimensions.viewport||dimensions.main>dimensions.mainWidth)throw Error('Overflow '+route);
   results.push({viewport:width,route,dimensions,console_errors:[...errors]});
  }
  if(errors.length)throw Error(JSON.stringify(errors));
  await context.close();
 }
 await browser.close();
 fs.writeFileSync(path.join(__dirname,'live-console-audit.json'),JSON.stringify(results,null,2));
 console.log('PASS: 22 real read-only route visits, zero console/page errors, no document or main overflow');
})().catch(error=>{console.error(error);process.exit(1)});
