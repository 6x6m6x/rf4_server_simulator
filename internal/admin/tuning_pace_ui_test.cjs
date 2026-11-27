// 饵效控速参数显隐与保存链路（我方逻辑）：好饵/差饵/海钓等待区间等 pace_* 行始终可见、
// 可直接改；开关只决定它们是否参与计算。保存走 POST /api/tunables。
const {test}=require('node:test');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
const webRoot=path.join(__dirname,'web');

function mockRoute(page,data,posts){
 page.route('**/*',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:fs.readFileSync(path.join(webRoot,'index.html'),'utf8')});
  if(url.pathname.startsWith('/app/')){
   const file=path.join(webRoot,url.pathname.replace(/^\//,''));
   if(fs.existsSync(file))return route.fulfill({contentType:file.endsWith('.css')?'text/css; charset=utf-8':'text/javascript; charset=utf-8',body:fs.readFileSync(file)});
   return route.fulfill({status:404,body:''});
  }
  if(url.pathname==='/api/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({username:'admin'})});
  if(url.pathname==='/api/tunables'&&route.request().method()==='POST'){posts.push(route.request().postDataJSON());return route.fulfill({contentType:'application/json',body:'{}'})}
  if(url.pathname==='/api/tunables')return route.fulfill({contentType:'application/json',body:JSON.stringify(data)});
  return route.fulfill({contentType:'application/json',body:'{}'});
 });
}

test('tuning always shows pace rows while bite pace disabled',async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
  const page=await context.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  mockRoute(page,{bite_pace_enabled:'0'},[]);
  await page.goto('http://127.0.0.1:19216/#tuning/bite');
  await page.getByRole('heading',{name:'数值调整',exact:true}).waitFor();
  await page.getByText('每次抛竿有鱼的概率',{exact:true}).waitFor();
  assert.equal(await page.getByText('好饵等待下限（饵效控速）',{exact:true}).count(),1,'开关关闭时好饵等待下限也应可见');
  assert.equal(await page.getByText('海钓等待下限·pace',{exact:true}).count(),1,'海钓等待上下限也应可见');
  await page.getByText('现在就能改，但要在开关开启后才参与计算').waitFor();
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close()}
});

test('tuning shows pace rows when enabled and saves through tunables API',async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
  const page=await context.newPage(),errors=[],posts=[];
  page.on('pageerror',e=>errors.push(e.message));
  mockRoute(page,{bite_pace_enabled:'1'},posts);
  await page.goto('http://127.0.0.1:19216/#tuning/bite');
  await page.getByRole('heading',{name:'数值调整',exact:true}).waitFor();
  await page.getByText('好饵等待下限（饵效控速）',{exact:true}).waitFor();
  assert.equal(await page.getByText('海钓等待上限·pace',{exact:true}).count(),1);
  await page.locator('[id^=f_pace_good_max_]').fill('45');
  await page.getByRole('button',{name:'保存修改',exact:true}).click();
  await page.getByText('已保存，约 30 秒内生效',{exact:true}).waitFor({timeout:5000});
  const hit=posts.find(p=>p&&p.key==='pace_good_max');
  assert.ok(hit,'应 POST pace_good_max');
  assert.equal(String(hit.value),'45');
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close()}
});
