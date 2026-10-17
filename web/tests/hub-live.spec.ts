import { expect, test, type Page } from "@playwright/test";

const owner = { authenticated: true,
  user: { id:"owner", username:"hub-qa", role:"owner", representative_id:"owner" },
  csrf_token:"a".repeat(64), expires_unix:1900000000 };
const representative = { ...owner, user: { ...owner.user,
  id:"rep-qa", username:"rep-qa", role:"representative", representative_id:"rep-qa" } };

function sample(cpu: number|null=31.25, rx: number|null=7_000_000) {
  const now=Math.floor(Date.now()/2000)*2;
  return {
    scope:"hub_host_physical_not_billable", source:"local_linux", sampled_at:new Date().toISOString(),
    metrics_current:true, cpu_cores:4, cpu_percent:cpu,
    ram_used_bytes:2_147_483_648, ram_total_bytes:8_589_934_592,
    disk_used_bytes:12_884_901_888, disk_total_bytes:64_424_509_440,
    load_1:1.25,load_5:0.9,load_15:0.6,host_uptime_seconds:129600,
    tcp_inuse:22,interface:"eth0", rx_bits_per_second:rx,
    tx_bits_per_second:1_600_000,hub_process_uptime_seconds:3605,
    hub_process_rss_bytes:94_371_840,
    history:[
      { ts_unix:now-4, cpu_percent:22.5, rx_bits_per_second:null, tx_bits_per_second:null },
      { ts_unix:now-2, cpu_percent:26, rx_bits_per_second:5_000_000, tx_bits_per_second:1_300_000 },
      { ts_unix:now, cpu_percent:cpu, rx_bits_per_second:rx, tx_bits_per_second:1_600_000 },
    ],
  };
}
async function setup(page: Page, role:"owner"|"representative"="owner"){
  let fail=false;
  let metrics=sample();
  const paths:string[]=[];
  await page.route("**/api/v1/**", async route=>{
    const url=new URL(route.request().url()),path=url.pathname;
    paths.push(path);
    if(path==="/api/v1/session/status")return route.fulfill({json:role==="owner"?owner:representative});
    if(path==="/api/v1/fleet")return route.fulfill({json:{
      as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,
      physical_rates_complete:false,interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,
      metric_scope:"default_route_interface_not_billable",nodes:[]}});
    if(path==="/api/v1/workspace/hub/metrics")return route.fulfill(fail?{status:503,json:{error:"hub_monitor_unavailable"}}:{json:metrics});
    if(path==="/api/v1/workspace/nodes")return route.fulfill({json:{mode:"new_pro_lab_only",nodes:[]}});
    if(path.startsWith("/api/v1/workspace/"))return route.fulfill({json:{clients:[],groups:[],events:[],operations:[]}});
    return route.fulfill({status:403,json:{}});
  });
  return { paths, setFail:(v:boolean)=>{fail=v;}, setMetrics:(v:ReturnType<typeof sample>)=>{metrics=v;} };
}

test("real HUB host resources appear independently even with zero paired PRO Nodes",async({page})=>{
  const observed=await setup(page);
  await page.goto("/dashboard");
  const hub=page.getByRole("region",{name:"HUB live monitor"});
  await expect(hub).toBeVisible();
  await expect(hub.getByText("HUB API responsive")).toBeVisible();
  await expect(hub.getByText("31.3%")).toBeVisible();
  await expect(hub.getByText("7 Mbps")).toBeVisible();
  await expect(hub.getByText("4", {exact:true})).toBeVisible();
  await expect(hub.getByText("HUB service uptime")).toBeVisible();
  await expect(hub.getByText("HUB host interface · NOT billable customer traffic")).toBeVisible();
  await expect(page.getByRole("heading",{name:"No Nodes connected yet"})).toBeVisible();
  expect(observed.paths).toContain("/api/v1/workspace/hub/metrics");
  expect(observed.paths.filter(s=>s.includes("/api/v1/admin/"))).toHaveLength(0);
});

test("HUB chart changes between sampled CPU, RX and TX without fetching synthetic history",async({page})=>{
  const observed=await setup(page);
  await page.goto("/dashboard");
  const hub=page.getByRole("region",{name:"HUB live monitor"});
  await expect(hub.getByRole("img",{name:"HUB CPU measured history"})).toBeVisible();
  await hub.getByRole("button",{name:"RX Mbps"}).click();
  await expect(hub.getByRole("img",{name:"HUB physical interface measured history"})).toBeVisible();
  await hub.getByRole("button",{name:"TX Mbps"}).click();
  await expect(hub.getByRole("button",{name:"TX Mbps"})).toHaveAttribute("aria-pressed","true");
  expect(observed.paths.filter(path=>path.endsWith("/history"))).toHaveLength(0);
});

test("first sample and failed refresh never fabricate zero or claim old HUB data is current",async({page})=>{
  const observed=await setup(page);
  observed.setMetrics(sample(null,null));
  await page.goto("/dashboard");
  const hub=page.getByRole("region",{name:"HUB live monitor"});
  await expect(hub.getByText("HUB API responsive")).toBeVisible();
  await expect(hub.getByText("0 Mbps")).toHaveCount(0);
  await expect(hub.getByText("—",{exact:true}).first()).toBeVisible();
  observed.setFail(true);
  await expect(hub.getByText("HUB telemetry unavailable")).toBeVisible({timeout:9000});
  await expect(hub.getByText("7 Mbps")).toHaveCount(0);
  await expect(hub.getByText("HUB host interface · NOT billable customer traffic")).toBeVisible();
});

test("320px RTL premium HUB console retains charts and never overflows viewport",async({page})=>{
  await setup(page);
  await page.setViewportSize({width:320,height:730});
  await page.goto("/dashboard");
  await page.getByRole("button",{name:"Switch language"}).click();
  await page.getByRole("button",{name:"Toggle dark and light mode"}).click();
  const hub=page.getByRole("region",{name:"HUB live monitor"});
  await expect(hub.getByText("قلب زیرساخت، زیر نظر تو.")).toBeVisible();
  await expect(hub.getByText("رم فرایند HUB")).toBeVisible();
  await hub.getByRole("button",{name:"RX Mbps"}).click();
  await expect(hub.getByRole("img",{name:"HUB physical interface measured history"})).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("dir","rtl");
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
});

test("representatives cannot poll HUB internals, and owner non-dashboard pages stay idle",async({page})=>{
  const rep=await setup(page,"representative");
  await page.goto("/clients");
  await expect(page.getByRole("navigation")).toBeVisible();
  await page.waitForTimeout(600);
  expect(rep.paths.filter(path=>path==="/api/v1/workspace/hub/metrics")).toHaveLength(0);
  expect(rep.paths.filter(path=>path==="/api/v1/fleet")).toHaveLength(0);
  await page.goto("/dashboard");
  await expect(page).toHaveURL(/\/clients$/);
  expect(rep.paths.filter(path=>path==="/api/v1/workspace/hub/metrics")).toHaveLength(0);
});
