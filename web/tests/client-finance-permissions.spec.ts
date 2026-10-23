import { expect, test } from "@playwright/test";

for (const viewFinance of [false, true]) {
 test(`representative client directory loads with view_finance=${viewFinance}`, async ({page}) => {
  const user={id:"rep-a",username:"rep-a",role:"representative",representative_id:"rep-a",
   permissions:{view_clients:true,view_finance:viewFinance,create_clients:false,
    renew_clients:false,edit_clients:false,manage_clients:false,manage_groups:false}};
  const client={id:"synthetic-client",name:"Directory client",representative_id:"rep-a",
   archived:false,deployment_state:"pending_node_apply",plan_kind:"volume",
   duration_days:30,ip_count:0,volume_bytes:1073741824,expires_unix:1900000000};
  const historyReason="Synthetic finance-protected operation";
  const requests:Array<{path:string;method:string;q:string}>=[];
  await page.route("**/api/v1/session/status",route=>route.fulfill({json:{
   authenticated:true,user,csrf_token:"a".repeat(64),expires_unix:1900000000,
  }}));
  await page.route("**/api/v1/fleet",route=>route.fulfill({json:{
   as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],
   metric_scope:"default_route_interface_not_billable",physical_rates_complete:false,
   interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,
  }}));
  await page.route("**/api/v1/workspace/**",route=>{
   const request=route.request(),url=new URL(request.url()),path=url.pathname;
   requests.push({path,method:request.method(),q:url.searchParams.get("q")??""});
   if(request.method()!=="GET")return route.fulfill({status:403,json:{error:"unexpected_mutation"}});
   if(path==="/api/v1/workspace/me")return route.fulfill({json:{
    user,data_plane:"not_activated",balance:{id:"rep-a",display_name:"Synthetic representative",
     kind:"representative",enabled:true,volume_credit_bytes:0,unlimited_credit_units:0},
   }});
   if(path==="/api/v1/workspace/clients"){
    const q=url.searchParams.get("q")??"";
    const clients=client.name.toLowerCase().includes(q.toLowerCase())?[client]:[];
    return route.fulfill({json:{clients,total:clients.length,has_more:false,
     limit:Number(url.searchParams.get("limit")),offset:Number(url.searchParams.get("offset")),
     q,state:url.searchParams.get("state"),plan:url.searchParams.get("plan"),
     sort:url.searchParams.get("sort"),representative_id:"rep-a"}});
   }
   if(path==="/api/v1/workspace/client-operations"){
    if(!viewFinance)return route.fulfill({status:403,json:{error:"business_permission_denied"}});
    return route.fulfill({json:{operations:[{event_id:"synthetic-operation",client_id:client.id,
     representative_id:"rep-a",kind:"extend",reason:historyReason,added_days:1,
     added_volume_bytes:0,volume_credit_debit:0,unlimited_credit_debit:1,created_unix:1800000000}]}});
   }
   return route.fulfill({status:404,json:{error:"not_found"}});
  });

  await page.goto("/clients");
  const directory=page.getByRole("region",{name:"Client directory"});
  await expect(directory.getByText(client.name,{exact:true})).toBeVisible();
  await directory.getByRole("searchbox").fill("Directory");
  await expect.poll(()=>requests.some(r=>r.path==="/api/v1/workspace/clients"&&r.q==="Directory")).toBe(true);
  await expect(directory.getByText(client.name,{exact:true})).toBeVisible();

  const history=page.getByRole("heading",{name:"Client operation history"});
  if(viewFinance){
   await expect(history).toBeVisible();
   await expect(page.getByText(historyReason,{exact:false})).toBeVisible();
   expect(requests.some(r=>r.path==="/api/v1/workspace/client-operations")).toBe(true);
  }else{
   await expect(history).toHaveCount(0);
   expect(requests.filter(r=>r.path==="/api/v1/workspace/client-operations")).toHaveLength(0);
  }
  expect(requests.filter(r=>r.method!=="GET")).toHaveLength(0);
 });
}
