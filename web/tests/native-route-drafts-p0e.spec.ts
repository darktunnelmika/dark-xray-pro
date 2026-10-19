import { expect, test } from "@playwright/test";

test("P0-E owner previews and saves bounded Dark Swap draft without runtime Apply (320px)",async({page})=>{
 const csrf="a".repeat(64),installationNL="a".repeat(32),installationDE="b".repeat(32);
 const writes:Array<{path:string;body:Record<string,unknown>;csrf:string}>=[];
 let saved:Record<string,unknown>|null=null;
 await page.setViewportSize({width:320,height:780});
 await page.route("**/api/v1/session/status",r=>r.fulfill({json:{
  authenticated:true,user:{id:"owner",username:"proowner",role:"owner",representative_id:"owner"},
  csrf_token:csrf,expires_unix:1900000000,
 }}));
 await page.route("**/api/v1/fleet",r=>r.fulfill({json:{
  as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],
  metric_scope:"default_route_interface_not_billable",physical_rates_complete:false,
  interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,
 }}));
 await page.route("**/api/v1/workspace/**",async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;
  if(path==="/api/v1/workspace/native-inbounds-state")
   return route.fulfill({json:{
    inbounds:[{id:301,name:"Native Test",revision:1,enabled:true,archived:false,
     transport:"tcp",grpc_service:"",reality_target:"example.org:443",
     server_names:["example.org"],short_ids:["aa11"],public_key:"TEST_PUBLIC",bound_clients:0}],
    nodes:[{node_id:"nl",installation_id:installationNL,name:"Netherlands"},
     {node_id:"de",installation_id:installationDE,name:"Germany"}],
    targets:[{inbound_id:301,node_id:"nl",installation_id:installationNL,
      listen_ip:"198.51.100.10",direct_port:0,swap_port:26000},
     {inbound_id:301,node_id:"de",installation_id:installationDE,
      listen_ip:"198.51.100.20",direct_port:21185,swap_port:26001}],
    runtime:[],
   }});
  if(path==="/api/v1/workspace/clients")return route.fulfill({json:{clients:[]}});
  if(path.endsWith("/clients"))return route.fulfill({json:{client_ids:[]}});
  if(path.endsWith("/operations"))return route.fulfill({json:{operations:[]}});
  if(path==="/api/v1/workspace/native-inbounds/301/route-drafts/de"&&req.method()==="GET")
   return saved?route.fulfill({json:saved}):route.fulfill({status:404,json:{error:"not_found"}});
  if(path.endsWith("/route-drafts/preview")&&req.method()==="POST"){
   const body=req.postDataJSON() as Record<string,unknown>;
   writes.push({path,body,csrf:req.headers()["x-csrf-token"]??""});
   return route.fulfill({json:{valid:true,apply_allowed:false,agent_ack_verified:false,
    data_plane_verified:false,billing_verified:false,subscription_published:false}});
  }
  if(path.endsWith("/route-drafts")&&req.method()==="POST"){
   const body=req.postDataJSON() as Record<string,unknown>;
   writes.push({path,body,csrf:req.headers()["x-csrf-token"]??""});
   saved={...body,draft_revision:1,listen_ip:"198.51.100.20",
    native_direct_port:21185,native_swap_port:26001,billing_source_node_id:"de",
    apply_allowed:false,agent_ack_verified:false,data_plane_verified:false,
    billing_verified:false,subscription_published:false};
   return route.fulfill({status:201,json:{draft:saved,
    clients_deleted:false,credit_changed:false,usage_reset:false,runtime_changed:false}});
  }
  return route.fulfill({status:404,json:{error:"not_found"}});
 });
 await page.goto("/");
 await page.getByRole("button",{name:"Open menu"}).click();
 await page.getByRole("button",{name:"Inbounds",exact:true}).click();
 await page.getByRole("button",{name:"Native Test",exact:false}).click();
 const editor=page.getByRole("region",{name:"Protected Inbound routing draft"});
 await editor.getByLabel("Route destination Node").selectOption("de");
 await expect(editor.getByText("No saved route draft for this Node.")).toBeVisible();
 await editor.getByLabel("Route Direct hosts").fill("de.example.org");
 await editor.getByLabel("Route Dark Swap").check();
 await editor.getByLabel("Route upstream Node").selectOption("nl");
 await editor.getByLabel("Route Swap host").fill("nl-tunnel.example.org");
 await editor.getByLabel("Route Swap tunnel port").fill("26000");
 await editor.getByLabel("Route Swap destination port").fill("26001");
 await editor.getByLabel("Route audit reason").fill("QA: owner-reviewed offline draft");
 await expect(editor.getByRole("button",{name:"Save protected draft"})).toBeDisabled();
 await editor.getByRole("button",{name:"Check ports and revision"}).click();
 await expect(editor.getByText("Preview valid; runtime Apply is still forbidden.")).toBeVisible();
 await editor.getByRole("button",{name:"Save protected draft"}).click();
 await expect(editor.getByText("Route draft saved; Agent/Xray acceptance still required.")).toBeVisible();
 expect(writes).toHaveLength(2);
 expect(writes[0].body).toMatchObject({inbound_id:301,node_id:"de",swap_enabled:true,
  swap_upstream_node_id:"nl",swap_tunnel_host:"nl-tunnel.example.org",
  swap_tunnel_port:26000,swap_node_port:26001});
 expect(writes[1].csrf).toBe(csrf);
 expect(writes[1].path).toBe("/api/v1/workspace/native-inbounds/301/route-drafts");
 expect(writes.some(x=>/native-nodes|client|payment|credit/.test(x.path))).toBe(false);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
});
