import { test, expect, type Page } from "@playwright/test";

const now=()=>Math.floor(Date.now()/60_000)*60;
const id="independent-new-qa";
async function setup(page: Page, role: "owner" | "representative" = "owner") {
  const user={id:role,username:"qa-node-owner",role,representative_id:role==="owner"?"owner":"rep-qa"};
  const session={authenticated:true,user,csrf_token:"a".repeat(64),expires_unix:1900000000};
  let signed=false;
  const windows:string[]=[];
  await page.route("**/api/v1/**",async route=>{
    const req=route.request();const url=new URL(req.url());const path=url.pathname;
    if(path.endsWith("/session/status"))return route.fulfill({json:signed?session:{authenticated:false,enabled:true}});
    if(path.endsWith("/session/login")){signed=true;return route.fulfill({json:session})}
    if(!signed)return route.fulfill({status:401,json:{error:"session_required"}});
    if(path.endsWith("/fleet"))return route.fulfill({json:{
      as_of:new Date().toISOString(),
      total_nodes:1,online_nodes:1,offline_nodes:0,physical_rates_complete:true,metric_scope:"default_route_interface_not_billable",
      interface_rx_bits_per_second:8000000,interface_tx_bits_per_second:1500000,
      nodes:[{node_id:id,name:"Fresh QA Node",interface:"eth0",status:"online",cpu_cores:4,cpu_percent:30,
        ram_total_bytes:8589934592,ram_used_bytes:4294967296,rx_bits_per_second:8000000,tx_bits_per_second:1500000,
        disk_total_bytes:53687091200,disk_used_bytes:10737418240,load_1:0.9,load_5:0.5,load_15:0.2,
        uptime_seconds:90061,tcp_inuse:25,metrics_current:true,last_seen:new Date().toISOString(),metrics_at:new Date().toISOString()}],
    }});
    if(path.endsWith("/workspace/nodes"))return route.fulfill({json:{mode:"new_pro_lab_only",nodes:[{node_id:id,name:"Fresh QA Node",paired_at:new Date().toISOString(),status:"online",metrics_current:true,last_seen:new Date().toISOString()}]}});
    if(path.endsWith("/history")){
      const w=url.searchParams.get("window");
      windows.push(String(w));
      const n=now();
      const points=w==="1h"?
        [{ts_unix:n-180,cpu_percent:20,ram_used_bytes:4294967296,ram_total_bytes:8589934592,disk_total_bytes:53687091200,disk_used_bytes:10737418240,load_1:0.8,tcp_inuse:23,rx_bits_per_second:null,tx_bits_per_second:null,observations:1},
         {ts_unix:n-120,cpu_percent:25,ram_used_bytes:4294967296,ram_total_bytes:8589934592,disk_total_bytes:53687091200,disk_used_bytes:10737418240,load_1:0.9,tcp_inuse:25,rx_bits_per_second:8000000,tx_bits_per_second:1500000,observations:1}]:
        [{ts_unix:n-7200,cpu_percent:12,ram_used_bytes:4294967296,ram_total_bytes:8589934592,disk_total_bytes:53687091200,disk_used_bytes:10737418240,load_1:0.8,tcp_inuse:21,rx_bits_per_second:1000000,tx_bits_per_second:400000,observations:5}];
      return route.fulfill({json:{node_id:id,window:w,bucket_seconds:w==="1h"?60:300,scope:"host_interface_not_billable",points}});
    }
    return route.fulfill({json:{clients:[],groups:[],events:[],operations:[],representatives:[]}});
  });
  return windows;
}
async function login(page:Page) {
 await page.goto("/nodes");
 await page.getByLabel("Username",{exact:true}).fill("qa-node-owner");
 await page.getByLabel("Password",{exact:true}).fill("private-test-only-password");
 await page.getByRole("button",{name:"Sign in",exact:true}).click();
}

test("Fresh Node inspector shows real optional disk, uptime, load, host TCP and persisted 1h/24h chart windows",async({page})=>{
 const queries=await setup(page);
 await login(page);
 await page.getByRole("button",{name:"Inspect Fresh QA Node"}).click();
 await expect(page.getByRole("region",{name:"Node details"})).toBeVisible();
 await expect(page.getByText("Root disk")).toBeVisible();
 await expect(page.getByText("Load (1m / 5m / 15m)")).toBeVisible();
 await expect(page.getByText("Host uptime")).toBeVisible();
 await expect(page.getByText("Host TCP sockets")).toBeVisible();
 const history=page.getByRole("region",{name:"Real Node history"});
 await expect(history).toBeVisible();
 await expect(history.getByText(/2 observed buckets/)).toBeVisible();
 await expect(history.locator("svg.pro-history-cpu")).toBeVisible();
 await history.getByRole("button",{name:"24 hours"}).click();
 await expect(history.getByText(/1 observed buckets/)).toBeVisible();
 expect(queries).toContain("1h");
 expect(queries).toContain("24h");
});
test("320px RTL dashboard does not overflow with unknown rather than zero fake history",async({page})=>{
 await setup(page);
 await page.setViewportSize({width:320,height:780});
 await login(page);
 await page.getByRole("button",{name:"Inspect Fresh QA Node"}).click();
 await page.getByRole("button",{name:"Switch language"}).click();
 await expect(page.getByText("دیسک ریشه")).toBeVisible();
 const history=page.getByRole("region",{name:"تاریخچهٔ واقعی نود"});
 await expect(history).toBeVisible();
 await expect(history.getByText(/مصرف قابل‌کسر مشتری نیست/)).toBeVisible();
 const width=await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth);
 expect(width).toBeLessThanOrEqual(2);
});
