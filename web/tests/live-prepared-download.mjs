import { chromium, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
const url=new URL(process.env.DARK_PRO_QA_UI_URL);
if(url.hostname!=="127.0.0.1"||url.protocol!=="http:")throw Error("loopback required");
const browser=await chromium.launch({args:["--no-zygote","--in-process-gpu","--disable-gpu"]});
async function login(user){
 const page=await browser.newPage();
 page.setDefaultTimeout(12000);
 page.on("response",r=>{if(r.url().includes("/dark-restore/handoff"))console.log(new URL(r.url()).pathname,r.status());});
 await page.route("**/*",route=>new URL(route.request().url()).origin===url.origin?route.continue():route.abort());
 await page.goto(new URL("/dark-restore",url).href);
 await page.getByLabel("Username",{exact:true}).fill(user);
 await page.getByLabel("Password",{exact:true}).fill(process.env.DARK_PRO_TEST_PASSWORD);
 await page.getByRole("button",{name:"Sign in",exact:true}).click();
 await expect(page.getByRole("heading",{name:"Client directory",exact:true})).toBeVisible();
 return page;
}
try{
 const page=await login("ownership-owner");
 const review=page.locator("button.darkrestore-review-head");
 if(await review.getAttribute("aria-expanded")==="false")await review.click();
 for(const name of ["Review server-approved handoff","Attach or recover receipt","Check activation readiness"])await page.getByRole("button",{name,exact:true}).click();
 await page.getByLabel("Inbound ID",{exact:true}).fill("801");
 await page.getByLabel("Plan revision",{exact:true}).fill("1");
 await page.getByLabel("Inbound revision",{exact:true}).fill("1");
 const path="/api/v1/workspace/dark-restore/handoff/prepare";
 await page.getByLabel("Plan revision",{exact:true}).fill("2");
 const [stale]=await Promise.all([page.waitForResponse(r=>new URL(r.url()).pathname===path),page.getByRole("button",{name:"Download prepared subscription"}).click()]);
 expect(stale.status()).toBeGreaterThanOrEqual(400);
 expect(await stale.json()).not.toHaveProperty("content");
 await page.getByLabel("Plan revision",{exact:true}).fill("1");
 let original,body;
 for(let i=0;i<2;i++){
  const [download,response]=await Promise.all([page.waitForEvent("download"),page.waitForResponse(r=>new URL(r.url()).pathname===path),page.getByRole("button",{name:"Download prepared subscription"}).click()]);
  expect(response.status()).toBe(200);
  body=response.request().postDataJSON();
  expect(download.suggestedFilename()).toBe("prepared-subscription.txt");
  const content=await readFile(await download.path(),"utf8");
  expect(content.startsWith("vless://"+process.env.DARK_PRO_TEST_UUID+"@prepared.example.org:21185")).toBe(true);
  if(i)expect(content===original).toBe(true);
  original=content;
 }
 const rep=await login("rep-target");
 const status=await rep.evaluate(async({path,body})=>{
  const session=await(await fetch("/api/v1/session/status")).json();
  return (await fetch(path,{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":session.csrf_token},body:JSON.stringify(body)})).status;
 },{path,body});
 expect(status).toBe(403);
}finally{await browser.close();}
