import { expect, test, type Page as BrowserPage } from "@playwright/test";
import type { Channel, Command, CommandRequest, MeteredCondition, Observation, Scope } from "../src/meteredRouteTransactions";
import type { Handover, HealthRound, Plan } from "../src/meteredHandovers";
import type { LabSnapshot, SubscriptionView } from "../src/labSubscriptions";
import type { ConsumerManifest, ConsumerView } from "../src/consumerManifests";

const base="/api/v1/workspace/metered-handovers",csrf="a".repeat(64),id=(n:number)=>n.toString(16).padStart(32,"0"),sha="d".repeat(64);
const zero=()=>({epoch_id:"",sequence:"0",sha256:""});
const stopped=():Observation=>({highwater:0,saved_sequence:0,saved_sha:"",active_sequence:0,active_sha:"",process_ready:false,phase:"awaiting_fresh_lease",lease_expires_unix_nano:0,data_plane_verified:false,subscription_published:false,no_listeners:false});
function row(node:string,install:string,n:number,action:Command["action"],stage:string):Command{return{id:id(n),node_id:node,installation_id:install,serial:n,action,stage_id:stage,state:"queued",created_unix_nano:Date.now()*1e6};}
function receipt(c:Command,outcome:string,o:Observation,u:MeteredCondition):Command{return{...c,state:"acknowledged",receipt:{version:2,command_id:c.id,serial:c.serial,action:c.action,stage_id:c.stage_id,node_id:c.node_id,installation_id:c.installation_id,runtime_instance:id(100),outcome,observed_unix_nano:Date.now()*1e6,observed:o,signature:sha,metered:{after:u,prior_settlement:outcome==="drained"?u.settlement:zero()}}};}

async function fixture(page:BrowserPage,role="owner"){
 const scope:Scope={enabled:true,scope:"metered_tcp_loopback_lab",node_id:"old-lab",installation_id:id(1),nodes:[{node_id:"old-lab",installation_id:id(1)},{node_id:"new-lab",installation_id:id(2)}]};
 const usage:MeteredCondition={epoch_id:id(41),epoch_sha:sha,admission_sha:sha,settlement:zero()};
 const view=(node:string,install:string):Channel=>({node_id:node,installation_id:install,binary_sha:sha,reported_at_unix_nano:Date.now()*1e6,observation_fresh:true,server_time_unix_milli:Date.now(),drafts_valid:true,status:{instance:id(100),serial:1,observed:stopped(),usage:{settlement:zero()}},review:[{inbound_id:7612,inbound_revision:1,draft_revision:1,name:"Shared LAB",listen_ip:"127.0.0.1",direct_port:node==="old-lab"?27612:27613,tunnel_port:0}],controls:{stage:true},commands:[]});
 const source=view("old-lab",id(1)),target=view("new-lab",id(2));
 source.status.current_stage_id=id(11);source.status.serial=2;source.status.usage=structuredClone(usage);source.status.observed={...stopped(),highwater:1,saved_sequence:1,saved_sha:sha,active_sequence:1,active_sha:sha,process_ready:true,phase:"ready",lease_expires_unix_nano:(Date.now()+30000)*1e6};source.controls={stage:true,drain:{stage_id:id(11),ports:[27612]}};
 target.status.staged_id=id(21);target.controls.apply={stage_id:id(21),ports:[27613],expires_unix_milli:Date.now()+90000};
 const state={scope,source,target,ops:[] as Handover[],post:"normal" as "normal"|"before"|"after",readError:false,fresh:true,malformed:false,
  clients:[{id:"client-a",name:"Synthetic A",archived:false},{id:"client-b",name:"Synthetic B",archived:false},{id:"client-archived",name:"Archived synthetic",archived:true}],
  subscriptions:{} as Record<string,SubscriptionView>,snapshotReceipts:{} as Record<string,LabSnapshot>,snapshotResponse:null as null|((v:LabSnapshot)=>unknown),snapshotWait:null as Promise<void>|null,
  consumer:null as ConsumerView|null,consumerReceipts:{} as Record<string,ConsumerManifest>,consumerResponse:null as null|((v:unknown)=>unknown)};
 for(const client of state.clients)state.subscriptions[client.id]={enabled:true,scope:"lab_single_handover",client_id:client.id,generation:1,state:"active",server_time_unix_milli:Date.now()};
 const writes:{path:string;raw:string;body:unknown;csrf:string}[]=[],reads:string[]=[];
 await page.route("**/api/v1/**",async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;
  if(path.endsWith("/session/status"))return route.fulfill({json:{authenticated:true,user:{id:role,username:"qa",role,representative_id:role},csrf_token:csrf,expires_unix:2000000000}});
  if(path==="/api/v1/fleet")return route.fulfill({json:{nodes:[],total_nodes:0,online_nodes:0,offline_nodes:0,as_of:new Date().toISOString()}});
  if(req.method()==="GET"){
   reads.push(path);
   if(path==="/api/v1/workspace/clients")return route.fulfill({json:{clients:state.clients,limit:25,offset:Number(new URL(req.url()).searchParams.get("offset")),has_more:false}});
   const subscription=path.match(/^\/api\/v1\/workspace\/clients\/([^/]+)\/subscription$/);
   if(subscription){const v=state.subscriptions[decodeURIComponent(subscription[1])];return route.fulfill(v?{json:v}:{status:404,json:{error:"not_found"}});}
   if(path==="/api/v1/workspace/nodes")return route.fulfill({json:{mode:"new_pro_lab_only",nodes:[],agent_transport_scope:"loopback_only"}});
   if(path==="/api/v1/workspace/metered-route-transactions")return route.fulfill({json:state.scope});
   if(path===base)return route.fulfill({json:{operations:state.ops,next_cursor:""}});
   if(path.endsWith("/consumer-manifest")){if(state.consumer?.enabled)state.consumer.server_time_unix_milli=Date.now();return route.fulfill(state.consumer?{json:state.consumer}:{status:404,json:{error:"not_found"}});}
   if(path.startsWith(base+"/")){const op=state.ops.find(o=>o.id===path.split("/").at(-1));return route.fulfill(op?{json:state.malformed?{...op,can_publish:true}:op}:{status:404,json:{error:"business_not_found"}});}
   const v=[source,target].find(v=>path===`/api/v1/workspace/native-nodes/${v.node_id}/metered-route-transactions`);
   if(v){if(state.readError)return route.fulfill({status:503,json:{error:"unavailable"}});v.server_time_unix_milli=Date.now();if(state.fresh)v.reported_at_unix_nano=Date.now()*1e6;return route.fulfill({json:v});}
   return route.fulfill({status:404,json:{error:"not_found"}});
  }
  const body=req.postDataJSON();writes.push({path,raw:req.postData()!,body,csrf:req.headers()["x-csrf-token"]??""});
  if(state.post==="before")return route.fulfill({status:503,json:{error:"unavailable"}});
  let reply:unknown;
  if(path===base){
   let op=state.ops.find(o=>o.id===body.id);
   if(!op){op={...(body as Plan),created_unix_nano:Date.now()*1e6,drain_command_id:"",apply_command_id:"",cancelled:false,can_publish:false,phase:"awaiting_source_drain"};state.ops.unshift(op);source.handover_id=op.id;target.handover_id=op.id;source.controls.stage=false;target.controls={stage:false};}reply=op;
  }else if(path.endsWith("/cancel")){
   const op=state.ops.find(o=>o.id===path.split("/").at(-2))!;if(op.drain_command_id)return route.fulfill({status:409,json:{error:"conflict"}});op.cancelled=true;op.phase="cancelled";reply=op;
  }else if(path.endsWith("/health")){
   const op=state.ops.find(o=>o.id===path.split("/").at(-2))!;
   let v=op.health_intents?.find(i=>i.id===body.id);
   if(!v){v={id:body.id,handover_id:op.id,node_id:op.target_node,installation_id:op.target_installation,inbound_id:body.inbound_id,endpoint_sha:sha,expires_unix_nano:(Date.now()+8000)*1e6};op.health_intents=[v,...(op.health_intents??[])];}reply=v;
  }else if(path.endsWith("/consumer-manifest")){
   const op=state.ops.find(o=>o.id===path.split("/").at(-2))!,head=state.consumer;
   if(!head?.enabled)return route.fulfill({status:409,json:{error:"disabled"}});
   let v=state.consumerReceipts[body.event_id];
   if(!v){
    if(body.expected_revision!==head.revision)return route.fulfill({status:409,json:{error:"revision_changed"}});
    v={id:id(4000+writes.length),event_id:body.event_id,handover_id:op.id,node_id:op.target_node,installation_id:op.target_installation,revision:head.revision+1,state:body.action==="revoke"?"revoked":"active",consumer_manifest_sha:(head.revision+1).toString(16).padStart(64,"0"),created_unix_nano:Date.now()*1e6,members:body.action==="revoke"?structuredClone(head.manifest!.members):body.members.map((m:{inbound_id:number;consumer_address:string;consumer_port:number})=>({...m,origin_port:target.review.find(r=>r.inbound_id===m.inbound_id)!.direct_port})),current:body.action!=="revoke"};
    state.consumerReceipts[body.event_id]=v;head.revision=v.revision;head.state=v.state;head.manifest=v;
    op.health_rounds?.forEach(r=>{r.current=false;r.state="state_changed";});
   }else v={...v,replayed:true,current:head.manifest?.id===v.id&&v.state==="active"};
   reply=state.consumerResponse?state.consumerResponse(structuredClone(v)):v;
  }else if(path.endsWith("/health-rounds")||path.endsWith("/consumer-health-rounds")){
   const op=state.ops.find(o=>o.id===path.split("/").at(-2))!;
   let v=op.health_rounds?.find(r=>r.id===body.id);
   if(!v){const created=Date.now()*1e6;v={id:body.id,handover_id:op.id,node_id:op.target_node,installation_id:op.target_installation,endpoint_sha:sha,manifest_sha:sha,created_unix_nano:created,expires_unix_nano:created+8e9,members:target.review.map((r,i)=>({inbound_id:r.inbound_id,intent_id:id(1000+(op.health_rounds?.length??0)*256+i),port:r.direct_port,state:"pending"})),state:"pending",current:false};
    if(path.endsWith("/consumer-health-rounds")){const m=state.consumer?.enabled?state.consumer.manifest:undefined;if(!m||m.id!==body.consumer_manifest_id||m.revision!==body.consumer_manifest_revision)return route.fulfill({status:409,json:{error:"manifest_changed"}});Object.assign(v,{consumer_manifest_id:m.id,consumer_manifest_revision:m.revision,consumer_manifest_sha:m.consumer_manifest_sha});v.members.forEach(row=>{const member=m.members.find(x=>x.inbound_id===row.inbound_id)!;Object.assign(row,{consumer_address:member.consumer_address,consumer_port:member.consumer_port});});}
    op.health_rounds=[v,...(op.health_rounds??[])];}reply=state.consumerResponse&&path.endsWith("/consumer-health-rounds")?state.consumerResponse(structuredClone(v)):v;
  }else if(path.endsWith("/lab-subscription-snapshot")){
   const op=state.ops.find(o=>o.id===path.split("/").at(-2))!,round=op.health_rounds!.find(r=>r.id===body.round_id)!;
   let v=state.snapshotReceipts[body.event_id];
   if(!v){v={id:id(3000+writes.length),event_id:body.event_id,client_id:body.client_id,handover_id:op.id,round_id:round.id,node_id:op.target_node,installation_id:op.target_installation,revision:body.expected_revision+1,created_unix_nano:Date.now()*1e6,expires_unix_nano:round.expires_unix_nano,route_count:round.members.length,current:round.current};if(round.consumer_manifest_id)Object.assign(v,{consumer_manifest_id:round.consumer_manifest_id,consumer_manifest_revision:round.consumer_manifest_revision,consumer_manifest_sha:round.consumer_manifest_sha});state.snapshotReceipts[body.event_id]=v;const sub=state.subscriptions[body.client_id];if(sub?.enabled)sub.snapshot=v;}
   else v={...v,replayed:true,current:v.current&&v.expires_unix_nano>Date.now()*1e6};
   reply=state.snapshotResponse?state.snapshotResponse(structuredClone(v)):v;
  }else{
   const input=body as CommandRequest,op=state.ops.find(o=>o.id===path.split("/").at(-2));
   const v=input.action==="drain"?source:target;
   let c=v.commands.find(c=>c.id===input.id);
   if(!c){c={...row(v.node_id,v.installation_id,v.status.serial+1,input.action,input.action==="stage"?input.id:input.stage_id),id:input.id};v.commands.unshift(c);}
   if(op&&input.action==="drain"){op.drain_command_id=c.id;op.drain_command=c;op.phase="source_drain_pending";}
   if(op&&input.action==="apply"){op.apply_command_id=c.id;op.apply_command=c;op.phase="target_apply_pending";}
   reply=c;
  }
  if(path.endsWith("/lab-subscription-snapshot")&&state.snapshotWait)await state.snapshotWait;
  return state.post==="after"?route.fulfill({status:503,json:{error:"unavailable"}}):route.fulfill({status:202,json:reply});
 });
 function settle(){const op=state.ops[0],u=structuredClone(usage);u.settlement={epoch_id:usage.epoch_id!,sequence:"2",sha256:sha};source.status.usage=u;source.status.observed={...source.status.observed,active_sequence:0,active_sha:"",process_ready:false,lease_expires_unix_nano:0};op.phase="source_drained";op.drain_command=receipt(op.drain_command!,"drained",source.status.observed,u);source.commands[0]=op.drain_command;source.status.serial=op.drain_command.serial;}
 return{state,writes,reads,settle};
}
const panel=(p:BrowserPage)=>p.getByRole("region",{name:"Metered Node transfer",exact:true});
async function open(p:BrowserPage){await p.goto("/nodes");await panel(p).getByRole("button",{name:"Open Node transfer",exact:true}).click();}
async function choose(p:BrowserPage){await panel(p).getByRole("combobox",{name:"Previous Node",exact:true}).selectOption("old-lab");await panel(p).getByRole("combobox",{name:"New Node",exact:true}).selectOption("new-lab");}
async function confirm(p:BrowserPage,name:string){p.once("dialog",d=>d.accept());await panel(p).getByRole("button",{name,exact:true}).click();}
async function refresh(p:BrowserPage){await panel(p).getByRole("button",{name:"Refresh transfer status",exact:true}).click();}
async function record(p:BrowserPage){await panel(p).getByRole("button",{name:"Review transfer",exact:true}).click();await confirm(p,"Record reviewed transfer");}

async function healthFixture(page:BrowserPage,rounds=false){
 const f=await fixture(page);f.state.scope.health_enabled=true;
 if(rounds){f.state.scope.health_rounds_enabled=true;for(const v of [f.state.source,f.state.target])v.review.push({...v.review[0],inbound_id:7613,name:"Second LAB",direct_port:v.review[0].direct_port+2});}
 const src=f.state.source,dst=f.state.target,usage=structuredClone(src.status.usage!);
 src.status.observed={...src.status.observed,process_ready:false,active_sequence:0,active_sha:"",lease_expires_unix_nano:0};
 src.status.usage={...usage,settlement:{epoch_id:usage.epoch_id!,sequence:"2",sha256:sha}};
 dst.status.current_stage_id=id(21);delete dst.status.staged_id;dst.status.serial=32;dst.status.usage={epoch_id:id(42),epoch_sha:sha,admission_sha:sha,settlement:zero()};
 dst.status.observed={...src.status.observed,highwater:1,saved_sequence:1,saved_sha:sha,active_sequence:1,active_sha:sha,process_ready:true,lease_expires_unix_nano:(Date.now()+60000)*1e6};
 const c=receipt(row(dst.node_id,dst.installation_id,32,"apply",id(21)),"applied",dst.status.observed,dst.status.usage);
 dst.commands=[c];dst.controls={stage:false};
 const op:Handover={id:id(88),source_node:src.node_id,target_node:dst.node_id,source_installation:src.installation_id,target_installation:dst.installation_id,source_stage_id:id(11),target_stage_id:id(21),source_usage:usage,created_unix_nano:Date.now()*1e6,drain_command_id:"",apply_command_id:c.id,apply_command:c,cancelled:false,can_publish:false,phase:"target_applied_health_required"};
 if(rounds)op.health_rounds=[];
 f.state.ops=[op];await open(page);await panel(page).getByRole("button",{name:"View transfer",exact:true}).click();
 if(!rounds)await panel(page).getByRole("combobox",{name:"Inbound to test",exact:true}).selectOption("7612");return {...f,op};
}

const roundAction="Test all destination Inbounds",roundCurrent="All destination Inbounds verified · current bounded protocol check";
function acceptMember(r:HealthRound,i:number){Object.assign(r.members[i],{state:"accepted",challenge_id:id(2000+i),accepted_unix_nano:Date.now()*1e6});}

test("N17 full-round request includes every destination Inbound and expires without publication",async({page})=>{
 const f=await healthFixture(page,true),button=panel(page).getByRole("button",{name:roundAction,exact:true});
 await expect(button).toBeEnabled();await expect(panel(page).getByRole("combobox",{name:"Inbound to test",exact:true})).toHaveCount(0);
 page.once("dialog",d=>d.dismiss());await button.click();expect(f.writes).toHaveLength(0);
 f.state.post="after";await confirm(page,roundAction);
 await expect(panel(page).getByText("Accepted Inbounds: 0/2",{exact:true})).toBeVisible();
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toHaveCount(0);
 expect(f.writes).toHaveLength(1);expect(f.writes[0].path).toBe(base+"/"+f.op.id+"/health-rounds");
 expect(f.writes[0].body).toEqual({id:expect.stringMatching(/^[a-f0-9]{32}$/),confirmed:true});expect(f.writes[0].csrf).toBe(csrf);
 const r=f.op.health_rounds![0];expect(r.members.map(m=>m.inbound_id)).toEqual([7612,7613]);
 acceptMember(r,0);await refresh(page);await expect(panel(page).getByText("Accepted Inbounds: 1/2",{exact:true})).toBeVisible();
 await expect(panel(page).getByText(roundCurrent,{exact:true})).toHaveCount(0);await expect(button).toBeDisabled();
 acceptMember(r,1);r.state="complete";r.current=true;await refresh(page);
 await expect(panel(page).getByText(roundCurrent,{exact:true})).toBeVisible();await expect(button).toBeDisabled();expect(f.op.can_publish).toBe(false);
 // Keep returning the same complete audit row: the local timer must still
 // remove the current claim at its original deadline, without a new receipt.
 await expect(panel(page).getByText("Round expired · current health is not confirmed",{exact:true})).toBeVisible({timeout:10000});
 await expect(panel(page).getByText(roundCurrent,{exact:true})).toHaveCount(0);await expect(button).toBeEnabled();expect(f.writes).toHaveLength(1);
});

test("N17 an unconfirmed round survives reload and retries only the exact saved request",async({page})=>{
 const f=await healthFixture(page,true);f.state.post="before";await confirm(page,roundAction);
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toBeEnabled();const raw=f.writes[0].raw;
 await page.reload();await panel(page).getByRole("button",{name:"Open Node transfer",exact:true}).click();
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toBeEnabled();expect(f.writes).toHaveLength(1);
 f.state.scope.health_rounds_enabled=false;await refresh(page);await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toBeDisabled();
 f.state.scope.health_rounds_enabled=true;f.state.post="normal";await refresh(page);await confirm(page,"Retry exact request");
 await expect.poll(()=>f.writes.length).toBe(2);expect(f.writes[1].raw).toBe(raw);expect(f.op.health_rounds).toHaveLength(1);
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toHaveCount(0);
});

test("N17 partial or mismatched round evidence blocks current health and controls",async({page})=>{
 const f=await healthFixture(page,true);await confirm(page,roundAction);await expect.poll(()=>f.op.health_rounds?.length).toBe(1);
 const original=structuredClone(f.op.health_rounds![0]);
 for(const mutate of [
  (r:HealthRound)=>{r.current=true;},
  (r:HealthRound)=>{r.members[1].inbound_id=r.members[0].inbound_id;},
  (r:HealthRound)=>{r.installation_id=id(99);},
 ]){
  f.op.health_rounds=[structuredClone(original)];await refresh(page);await expect(panel(page).getByRole("alert")).toHaveCount(0);
  const bad=structuredClone(original);mutate(bad);f.op.health_rounds=[bad];await refresh(page);
  await expect(panel(page).getByRole("alert")).toContainText("could not be verified");
  await expect(panel(page).getByRole("button",{name:roundAction,exact:true})).toBeDisabled();await expect(panel(page).getByText(roundCurrent,{exact:true})).toHaveCount(0);
 }
 expect(f.writes).toHaveLength(1);
});

test("N17 complete evidence for a subset never claims full destination coverage",async({page})=>{
 const f=await healthFixture(page,true);await confirm(page,roundAction);await expect.poll(()=>f.op.health_rounds?.length).toBe(1);
 const r=f.op.health_rounds![0];r.members.pop();acceptMember(r,0);r.state="complete";r.current=true;await refresh(page);
 await expect(panel(page).getByText("Complete receipt set recorded · current health is not confirmed",{exact:true})).toBeVisible();
 await expect(panel(page).getByText(roundCurrent,{exact:true})).toHaveCount(0);expect(f.op.can_publish).toBe(false);
});

test("N17 a read failure or changed state removes a previously current round claim",async({page})=>{
 const f=await healthFixture(page,true);await confirm(page,roundAction);await expect.poll(()=>f.op.health_rounds?.length).toBe(1);
 const r=f.op.health_rounds![0];r.members.forEach((_,i)=>acceptMember(r,i));r.state="complete";r.current=true;await refresh(page);
 await expect(panel(page).getByText(roundCurrent,{exact:true})).toBeVisible();
 f.state.readError=true;await refresh(page);await expect(panel(page).getByRole("alert")).toBeVisible();await expect(panel(page).getByText(roundCurrent,{exact:true})).toHaveCount(0);
 f.state.readError=false;r.current=false;r.state="state_changed";await refresh(page);
 await expect(panel(page).getByText("Destination or source state changed · this round is no longer current",{exact:true})).toBeVisible();
 await expect(panel(page).getByRole("button",{name:roundAction,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(1);
});

test("N17 full-round review remains readable in Persian at 320 pixels",async({page})=>{
 await page.setViewportSize({width:320,height:780});const f=await healthFixture(page,true);await confirm(page,roundAction);
 await expect(panel(page).getByText("Accepted Inbounds: 0/2",{exact:true})).toBeVisible();
 await page.getByRole("button",{name:"Switch language",exact:true}).click();
 await expect(panel(page).getByRole("button",{name:"آزمون همهٔ اینباندهای مقصد",exact:true})).toBeVisible();
 await expect(panel(page).getByRole("heading",{name:"دوره‌های آزمون سلامت مقصد",exact:true})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(f.writes).toHaveLength(1);
});

test("N15 explicit health action reconciles a lost committed reply and shows historical evidence",async({page})=>{
 const f=await healthFixture(page);const button=panel(page).getByRole("button",{name:"Request protocol health test",exact:true});
 await expect(button).toBeEnabled();expect(f.writes).toHaveLength(0);
 page.once("dialog",d=>d.dismiss());await button.click();expect(f.writes).toHaveLength(0);
 f.state.post="after";await confirm(page,"Request protocol health test");
 await expect(panel(page).getByText("Health request recorded · waiting for Agent or receipt",{exact:true})).toBeVisible();
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toHaveCount(0);
 expect(f.writes).toHaveLength(1);expect(f.writes[0].path).toBe(base+"/"+f.op.id+"/health");expect(Object.keys(f.writes[0].body as object).sort()).toEqual(["confirm_probe","id","inbound_id"]);
 await expect(button).toBeDisabled();f.op.historical_health_evidence={challenge_id:id(89),handover_id:f.op.id,request_sha:sha,receipt_sha:sha,accepted_unix_nano:Date.now()*1e6,replayed:false};
 await refresh(page);await expect(panel(page).getByText("Historical protocol health receipt recorded · no subscription published",{exact:true})).toBeVisible();
 await expect(button).toBeDisabled();expect(f.op.can_publish).toBe(false);expect(f.writes).toHaveLength(1);
});

test("N15 uncommitted health action survives reload and retries the exact body",async({page})=>{
 const f=await healthFixture(page);f.state.post="before";await confirm(page,"Request protocol health test");
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toBeEnabled();const raw=f.writes[0].raw;
 await page.reload();await panel(page).getByRole("button",{name:"Open Node transfer",exact:true}).click();
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toBeEnabled();expect(f.writes).toHaveLength(1);
 f.state.post="normal";await confirm(page,"Retry exact request");await expect.poll(()=>f.writes.length).toBe(2);expect(f.writes[1].raw).toBe(raw);expect(f.writes[1].csrf).toBe(csrf);
});

test("N10 explicit review, source final receipt and separate target request",async({page})=>{
 const f=await fixture(page);await open(page);await choose(page);expect(f.writes).toHaveLength(0);
 await panel(page).getByRole("button",{name:"Review transfer",exact:true}).click();
 page.once("dialog",d=>d.dismiss());await panel(page).getByRole("button",{name:"Record reviewed transfer",exact:true}).click();expect(f.writes).toHaveLength(0);
 await confirm(page,"Record reviewed transfer");await expect(panel(page).getByRole("button",{name:"Stop and settle previous Node",exact:true})).toBeEnabled();
 expect(f.writes[0].body).toMatchObject({source_node:"old-lab",target_node:"new-lab",source_installation:id(1),target_installation:id(2),source_stage_id:id(11),target_stage_id:id(21),source_usage:{epoch_id:id(41),settlement:{sequence:"0"}}});
 await expect(panel(page).getByRole("button",{name:"Request destination execution",exact:true})).toBeDisabled();
 await confirm(page,"Stop and settle previous Node");await expect(panel(page).getByText("Source stop queued · receipt pending",{exact:true}).first()).toBeVisible();
 await expect(panel(page).getByRole("button",{name:"Request destination execution",exact:true})).toBeDisabled();expect(f.writes).toHaveLength(2);
 f.settle();f.state.fresh=false;f.state.source.reported_at_unix_nano=(Date.now()-13000)*1e6;f.state.target.reported_at_unix_nano=(Date.now()-13000)*1e6;await refresh(page);
 await expect(panel(page).getByText("Source stop and settlement ACK recorded",{exact:true}).first()).toBeVisible();
 await expect(panel(page).getByText("Current process state unconfirmed · observation stale",{exact:true}).first()).toBeVisible();
 await expect(panel(page).getByRole("button",{name:"Request destination execution",exact:true})).toBeDisabled();
 f.state.fresh=true;await refresh(page);await expect(panel(page).getByRole("button",{name:"Request destination execution",exact:true})).toBeEnabled();
 expect(f.writes).toHaveLength(2);await confirm(page,"Request destination execution");
 await expect.poll(()=>f.writes.length).toBe(3);expect(f.writes[2].body).toMatchObject({action:"apply",stage_id:id(21),drafts:[],usage:{settlement:{sequence:"0"}}});
 expect(f.writes.every(w=>w.csrf===csrf)).toBe(true);await expect(panel(page).getByText(/Public subscription publication and cross-Node rollback remain unavailable/)).toBeVisible();
});

test("N10 retained create survives reload with identical ID/body and no automatic retry",async({page})=>{
 const f=await fixture(page);f.state.post="before";await open(page);await choose(page);await record(page);
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toBeEnabled();const original=f.writes[0].raw;
 await page.reload();await panel(page).getByRole("button",{name:"Open Node transfer",exact:true}).click();
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toBeEnabled();expect(f.writes).toHaveLength(1);
 f.state.post="normal";await confirm(page,"Retry exact request");await expect.poll(()=>f.writes.length).toBe(2);expect(f.writes[1].raw).toBe(original);expect(f.state.ops).toHaveLength(1);
});

test("N10 lost command reply reconciles journal without another command",async({page})=>{
 const f=await fixture(page);await open(page);await choose(page);await record(page);await expect(panel(page).getByRole("button",{name:"Stop and settle previous Node",exact:true})).toBeEnabled();
 f.state.post="after";await confirm(page,"Stop and settle previous Node");await expect(panel(page).getByText("Source stop queued · receipt pending",{exact:true}).first()).toBeVisible();
 await expect(panel(page).getByRole("button",{name:"Retry exact request",exact:true})).toHaveCount(0);expect(f.writes).toHaveLength(2);
 await refresh(page);expect(f.writes).toHaveLength(2);await expect(panel(page).getByRole("button",{name:"Cancel before source stop",exact:true})).toBeDisabled();
});

test("N10 preparation and failed reads cannot execute another step",async({page})=>{
 const f=await fixture(page);delete f.state.target.status.staged_id;delete f.state.target.controls.apply;await open(page);await choose(page);
 await expect(panel(page).getByRole("button",{name:"Review transfer",exact:true})).toBeDisabled();await confirm(page,"Prepare destination");
 await expect.poll(()=>f.writes.length).toBe(1);expect(f.writes[0].body).toMatchObject({action:"stage",drafts:[{inbound_id:7612,inbound_revision:1,draft_revision:1}]});
 f.state.readError=true;await refresh(page);await expect(panel(page).getByRole("alert")).toBeVisible();await expect(panel(page).getByRole("button",{name:"Prepare destination",exact:true})).toBeDisabled();expect(f.writes).toHaveLength(1);
});

test("N10 storage failure prevents POST and an unverified publication claim blocks controls",async({page})=>{
 const f=await fixture(page);await open(page);await choose(page);
 await page.evaluate(()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k.startsWith("dark-pro-handover-attempt:"))throw new Error("quota");return original.call(this,k,v);};});
 await panel(page).getByRole("button",{name:"Review transfer",exact:true}).click();await confirm(page,"Record reviewed transfer");
 await expect(panel(page).getByRole("alert")).toContainText("could not be saved");expect(f.writes).toHaveLength(0);
 await page.reload();await panel(page).getByRole("button",{name:"Open Node transfer",exact:true}).click();await choose(page);await record(page);
 await expect(panel(page).getByRole("button",{name:"Stop and settle previous Node",exact:true})).toBeEnabled();f.state.malformed=true;await refresh(page);
 await expect(panel(page).getByRole("alert")).toContainText("could not be verified");await expect(panel(page).getByRole("button",{name:"Stop and settle previous Node",exact:true})).toBeDisabled();expect(f.writes).toHaveLength(1);
});

test("N10 Persian 320px, disabled runtime and representative boundary",async({page})=>{
 const f=await fixture(page);f.state.scope.enabled=false;f.state.scope.nodes=[];delete f.state.scope.node_id;delete f.state.scope.installation_id;
 await page.setViewportSize({width:320,height:780});await open(page);await expect(panel(page).getByText(/Node execution is disabled/)).toBeVisible();
 await page.getByRole("button",{name:"Switch language",exact:true}).click();await expect(panel(page).getByRole("heading",{name:"انتقال بین نودهای LAB",exact:true})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(f.writes).toHaveLength(0);
});

test("N10 representative never loads transfer APIs",async({page})=>{
 const f=await fixture(page,"representative");await page.goto("/nodes");await expect(panel(page)).toHaveCount(0);expect(f.reads.some(p=>p.startsWith(base))).toBe(false);expect(f.writes).toHaveLength(0);
});

const subscriptionPanel=(p:BrowserPage)=>p.getByRole("region",{name:"LAB subscription snapshot",exact:true});
const recordSnapshot="Record LAB subscription snapshot",retrySnapshot="Retry exact snapshot request",currentSnapshot="Current LAB delivery verified at recording";
async function subscriptionFixture(page:BrowserPage,enabled=true){
 const f=await healthFixture(page,true);f.state.scope.lab_subscriptions_enabled=enabled;
 const created=Date.now()*1e6;
 const round:HealthRound={id:id(500),handover_id:f.op.id,node_id:f.op.target_node,installation_id:f.op.target_installation,endpoint_sha:sha,manifest_sha:sha,created_unix_nano:created,expires_unix_nano:created+8e9,state:"complete",current:true,members:f.state.target.review.map((r,i)=>({inbound_id:r.inbound_id,port:r.direct_port,intent_id:id(600+i),state:"accepted",challenge_id:id(700+i),accepted_unix_nano:created}))};
 f.op.health_rounds=[round];await refresh(page);return{...f,round};
}
async function selectSubscriptionClient(page:BrowserPage,client="client-a"){
 await subscriptionPanel(page).getByRole("combobox",{name:"Subscription client",exact:true}).selectOption(client);
}
async function recordSubscription(page:BrowserPage){
 page.once("dialog",d=>d.accept());await subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true}).click();
}

test("N18 snapshot feature off stays hidden; active client token and current revision require explicit confirmation",async({page})=>{
 const f=await subscriptionFixture(page,false);
 await expect(subscriptionPanel(page)).toHaveCount(0);expect(f.reads.some(p=>p==="/api/v1/workspace/clients")).toBe(false);expect(f.writes).toHaveLength(0);
 f.state.scope.lab_subscriptions_enabled=true;await refresh(page);
 const box=subscriptionPanel(page),button=box.getByRole("button",{name:recordSnapshot,exact:true});
 await expect(button).toBeDisabled();expect(f.writes).toHaveLength(0);
 const archived=box.getByRole("option",{name:"Archived synthetic (client-archived)",exact:true});await expect(archived).toHaveJSProperty("disabled",true);
 const revoked=f.state.subscriptions["client-b"];if(revoked.enabled){revoked.generation=2;revoked.state="revoked";}
 await selectSubscriptionClient(page,"client-b");await expect(box.getByText(/Issue a subscription token/)).toBeVisible();await expect(button).toBeDisabled();
 const active=f.state.subscriptions["client-a"],oldCreated=(Date.now()-10000)*1e6;
 if(active.enabled)active.snapshot={id:id(799),event_id:id(798),client_id:"client-a",handover_id:f.op.id,round_id:id(797),node_id:f.op.target_node,installation_id:f.op.target_installation,revision:4,created_unix_nano:oldCreated,expires_unix_nano:oldCreated+7e9,route_count:2,current:false};
 await selectSubscriptionClient(page);await expect(button).toBeEnabled();expect(f.writes).toHaveLength(0);
 page.once("dialog",d=>d.dismiss());await button.click();expect(f.writes).toHaveLength(0);
 await recordSubscription(page);await expect(box.getByText(currentSnapshot,{exact:true})).toBeVisible();
 expect(f.writes).toHaveLength(1);expect(f.writes[0].path).toBe(base+"/"+f.op.id+"/lab-subscription-snapshot");expect(f.writes[0].csrf).toBe(csrf);
 expect(f.writes[0].body).toEqual({event_id:expect.stringMatching(/^[a-f0-9]{32}$/),client_id:"client-a",round_id:f.round.id,expected_revision:4});
 const saved=Object.values(f.state.snapshotReceipts)[0];expect(saved.id).not.toBe(saved.event_id);expect(saved.revision).toBe(5);expect(saved.expires_unix_nano).toBe(f.round.expires_unix_nano);expect(f.op.can_publish).toBe(false);
});

test("N18 complete evidence for a subset cannot enable snapshot recording",async({page})=>{
 const f=await subscriptionFixture(page);f.round.members.pop();await refresh(page);await selectSubscriptionClient(page);
 await expect(subscriptionPanel(page).getByText(/complete, current health round for every destination Inbound/)).toBeVisible();
 await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(0);
});

test("N18 exact nonsecret snapshot retry survives reload and remains scoped to its original client and round",async({page})=>{
 const f=await subscriptionFixture(page);await selectSubscriptionClient(page);f.state.post="before";await recordSubscription(page);
 await expect(subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();
 const raw=f.writes[0].raw,body=f.writes[0].body as {event_id:string;client_id:string;round_id:string;expected_revision:number};
 const retained=await page.evaluate(()=>Object.entries(sessionStorage).filter(([key])=>key.startsWith("dark-pro-lab-snapshot-attempt:")));
 expect(retained).toHaveLength(1);expect(JSON.parse(retained[0][1])).toEqual(body);expect(Object.keys(JSON.parse(retained[0][1])).sort()).toEqual(["client_id","event_id","expected_revision","round_id"]);
 await page.reload();await panel(page).getByRole("button",{name:"Open Node transfer",exact:true}).click();await panel(page).getByRole("button",{name:"View transfer",exact:true}).click();
 await expect(subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();
 await expect(subscriptionPanel(page).getByRole("combobox",{name:"Subscription client",exact:true})).toHaveValue("client-a");await expect(subscriptionPanel(page).getByRole("combobox",{name:"Subscription client",exact:true})).toBeDisabled();expect(f.writes).toHaveLength(1);
 f.state.post="normal";await subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true}).click();await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toBeVisible();
 expect(f.writes).toHaveLength(2);expect(f.writes[1].raw).toBe(raw);expect(f.writes[1].csrf).toBe(csrf);
 expect(await page.evaluate(()=>Object.keys(sessionStorage).filter(k=>k.startsWith("dark-pro-lab-snapshot-attempt:")))).toHaveLength(0);
});

test("N18 changing transfer never reuses a retained snapshot request",async({page})=>{
 const f=await subscriptionFixture(page);await selectSubscriptionClient(page);f.state.post="before";await recordSubscription(page);
 await expect(subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();
 const next=structuredClone(f.op);next.id=id(89);next.health_rounds!.forEach(r=>{r.id=id(501);r.handover_id=next.id;});f.state.ops.push(next);
 await refresh(page);await expect(panel(page).getByRole("button",{name:"View transfer",exact:true})).toHaveCount(2);await panel(page).getByRole("button",{name:"View transfer",exact:true}).last().click();
 await expect(panel(page).getByRole("article",{name:"Selected transfer",exact:true}).getByText(next.id,{exact:true})).toBeVisible();
 await expect(subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true})).toHaveCount(0);await expect(subscriptionPanel(page).getByRole("combobox",{name:"Subscription client",exact:true})).toHaveValue("");
 expect(f.writes).toHaveLength(1);expect(await page.evaluate(()=>Object.keys(sessionStorage).filter(k=>k.startsWith("dark-pro-lab-snapshot-attempt:")))).toHaveLength(1);
});

for(const [name,mutate] of [
 ["event",(v:LabSnapshot)=>{v.event_id=id(901);}],["client",(v:LabSnapshot)=>{v.client_id="client-b";}],
 ["round",(v:LabSnapshot)=>{v.round_id=id(902);}],["operation",(v:LabSnapshot)=>{v.handover_id=id(903);}],
 ["original deadline",(v:LabSnapshot)=>{v.created_unix_nano+=1e9;v.expires_unix_nano+=1e9;}],
] as const){
 test(`N18 a mismatched ${name} snapshot receipt remains unconfirmed`,async({page})=>{
  const f=await subscriptionFixture(page);f.state.snapshotResponse=v=>{mutate(v);return v;};await selectSubscriptionClient(page);await recordSubscription(page);
  await expect(subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toHaveCount(0);
  expect(f.writes).toHaveLength(1);expect(await page.evaluate(()=>Object.keys(sessionStorage).filter(k=>k.startsWith("dark-pro-lab-snapshot-attempt:")))).toHaveLength(1);
 });
}

test("N18 metadata for a different client cannot enable recording",async({page})=>{
 const f=await subscriptionFixture(page);f.state.subscriptions["client-a"]={...f.state.subscriptions["client-b"]};await selectSubscriptionClient(page);
 await expect(subscriptionPanel(page).getByRole("alert")).toBeVisible();await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(0);
});

test("N18 a recorded result loses its current claim after round change, failed observation and scope withdrawal",async({page})=>{
 const f=await subscriptionFixture(page);await selectSubscriptionClient(page);await recordSubscription(page);
 const box=subscriptionPanel(page);await expect(box.getByText(currentSnapshot,{exact:true})).toBeVisible();
 const old=structuredClone(f.round);f.round.current=false;f.round.state="state_changed";await refresh(page);
 await expect(box.getByText(currentSnapshot,{exact:true})).toHaveCount(0);await expect(box.getByText("Snapshot recorded; current delivery not confirmed",{exact:true})).toBeVisible();
 Object.assign(f.round,old);await refresh(page);await expect(box.getByText(currentSnapshot,{exact:true})).toBeVisible();
 f.state.readError=true;await refresh(page);await expect(box.getByText(currentSnapshot,{exact:true})).toHaveCount(0);
 f.state.readError=false;f.state.scope.enabled=false;f.state.scope.nodes=[];delete f.state.scope.node_id;delete f.state.scope.installation_id;await refresh(page);
 await expect(box.getByText(currentSnapshot,{exact:true})).toHaveCount(0);await expect(box.getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(1);
});

test("N18 confirmation cannot send after the original health deadline passes",async({page})=>{
 const f=await subscriptionFixture(page);await selectSubscriptionClient(page);
 f.round.expires_unix_nano=(Date.now()+1400)*1e6;await refresh(page);await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeEnabled();
 page.once("dialog",async d=>{await new Promise(resolve=>setTimeout(resolve,1700));await d.accept();});
 await subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true}).click();
 await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(0);
});

test("N18 a lost committed snapshot can be retried historically without extending its deadline or showing current delivery",async({page})=>{
 const f=await subscriptionFixture(page);await selectSubscriptionClient(page);f.round.expires_unix_nano=(Date.now()+2200)*1e6;await refresh(page);
 f.state.post="after";await recordSubscription(page);const box=subscriptionPanel(page);
 await expect(box.getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();const original=f.writes[0].raw,deadline=Object.values(f.state.snapshotReceipts)[0].expires_unix_nano;
 await expect(box.getByText(/complete, current health round for every destination Inbound/)).toBeVisible({timeout:4000});
 await expect(box.getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();f.state.post="normal";await box.getByRole("button",{name:retrySnapshot,exact:true}).click();
 await expect(box.getByText("Snapshot recorded; current delivery not confirmed",{exact:true})).toBeVisible();await expect(box.getByText(currentSnapshot,{exact:true})).toHaveCount(0);
 expect(f.writes).toHaveLength(2);expect(f.writes[1].raw).toBe(original);expect(Object.values(f.state.snapshotReceipts)[0].expires_unix_nano).toBe(deadline);expect(f.op.can_publish).toBe(false);
});

test("N18 snapshot storage failure prevents POST and narrow Persian remains readable",async({page})=>{
 await page.setViewportSize({width:320,height:780});const f=await subscriptionFixture(page);await selectSubscriptionClient(page);
 await page.evaluate(()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(key,value){if(key.startsWith("dark-pro-lab-snapshot-attempt:"))throw Error("quota");return original.call(this,key,value);};});
 await recordSubscription(page);await expect(subscriptionPanel(page).getByRole("alert")).toBeVisible();expect(f.writes).toHaveLength(0);
 await page.getByRole("button",{name:"Switch language",exact:true}).click();
 await expect(subscriptionPanel(page).getByRole("heading",{name:"آماده‌سازی خروجی اشتراک LAB",exact:true})).toBeVisible();await expect(subscriptionPanel(page).getByRole("button",{name:"ثبت نسخهٔ اشتراک LAB",exact:true})).toBeDisabled();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

const consumerPanel=(p:BrowserPage)=>p.getByRole("region",{name:"Consumer endpoint manifest",exact:true});
const recordConsumer="Record consumer mapping",proveConsumer="Test approved consumer routes",retryConsumer="Retry exact consumer request";
async function consumerFixture(page:BrowserPage,active=false){
 const f=await healthFixture(page,true);f.state.scope.lab_subscriptions_enabled=true;f.state.scope.consumer_manifests_enabled=true;
 f.state.consumer={enabled:true,scope:"consumer_endpoint_lab",handover_id:f.op.id,revision:0,state:"absent",server_time_unix_milli:Date.now(),required_inbounds:f.state.target.review.map(r=>({inbound_id:r.inbound_id,origin_port:r.direct_port})),approved_endpoints:f.state.target.review.flatMap((r,i)=>[0,10].map(delta=>({node_id:f.op.target_node,installation_id:f.op.target_installation,inbound_id:r.inbound_id,address:"11.0.0.2",port:28613+i*2+delta})))};
 if(active){const v=f.state.consumer;v.revision=1;v.state="active";v.manifest={id:id(4001),event_id:id(3999),handover_id:f.op.id,node_id:f.op.target_node,installation_id:f.op.target_installation,revision:1,state:"active",consumer_manifest_sha:"1".padStart(64,"0"),created_unix_nano:Date.now()*1e6,members:v.required_inbounds.map((r,i)=>({...r,consumer_address:"11.0.0.2",consumer_port:28613+i*2})),current:true};}
 await refresh(page);await expect(consumerPanel(page).getByRole("button",{name:recordConsumer,exact:true})).toBeVisible();
 return f;
}
function completeConsumerRound(f:Awaited<ReturnType<typeof consumerFixture>>,remaining=8000){
 const m=f.state.consumer!.enabled?f.state.consumer!.manifest!:null!;
 const created=(Date.now()-(8000-remaining))*1e6;
 const round:HealthRound={id:id(500),handover_id:f.op.id,node_id:f.op.target_node,installation_id:f.op.target_installation,endpoint_sha:sha,manifest_sha:sha,consumer_manifest_id:m.id,consumer_manifest_revision:m.revision,consumer_manifest_sha:m.consumer_manifest_sha,created_unix_nano:created,expires_unix_nano:created+8e9,state:"complete",current:true,members:m.members.map((r,i)=>({inbound_id:r.inbound_id,port:r.origin_port,consumer_address:r.consumer_address,consumer_port:r.consumer_port,intent_id:id(600+i),state:"accepted",challenge_id:id(700+i),accepted_unix_nano:created}))};
 f.op.health_rounds=[round];return round;
}
async function chooseConsumer(page:BrowserPage){await consumerPanel(page).getByRole("combobox",{name:"Consumer endpoint 7612",exact:true}).selectOption("11.0.0.2:28613");await consumerPanel(page).getByRole("combobox",{name:"Consumer endpoint 7613",exact:true}).selectOption("11.0.0.2:28615");}

test("N19 complete immutable mapping and explicit consumer proof are required before output",async({page})=>{
 const f=await consumerFixture(page),box=consumerPanel(page);
 expect(f.writes).toHaveLength(0);await expect(panel(page).getByRole("button",{name:roundAction,exact:true})).toHaveCount(0);
 await expect(box.getByRole("button",{name:recordConsumer,exact:true})).toBeDisabled();
 await box.getByRole("combobox",{name:"Consumer endpoint 7612",exact:true}).selectOption("11.0.0.2:28613");await expect(box.getByRole("button",{name:recordConsumer,exact:true})).toBeDisabled();
 await chooseConsumer(page);page.once("dialog",d=>d.dismiss());await box.getByRole("button",{name:recordConsumer,exact:true}).click();expect(f.writes).toHaveLength(0);
 await confirm(page,recordConsumer);await expect(box.getByText("Recorded mapping matches the current destination",{exact:true})).toBeVisible();
 const manifest=f.state.consumer!.enabled?f.state.consumer!.manifest!:null!;
 expect(f.writes[0].path).toBe(base+"/"+f.op.id+"/consumer-manifest");expect(f.writes[0].body).toEqual({event_id:expect.stringMatching(/^[a-f0-9]{32}$/),action:"replace",expected_revision:0,members:[{inbound_id:7612,consumer_address:"11.0.0.2",consumer_port:28613},{inbound_id:7613,consumer_address:"11.0.0.2",consumer_port:28615}],confirmed:true});expect(f.writes[0].csrf).toBe(csrf);
 await selectSubscriptionClient(page);await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();
 await confirm(page,proveConsumer);await expect.poll(()=>f.op.health_rounds?.length).toBe(1);expect(f.writes[1].path).toBe(base+"/"+f.op.id+"/consumer-health-rounds");expect(f.writes[1].body).toEqual({id:expect.stringMatching(/^[a-f0-9]{32}$/),consumer_manifest_id:manifest.id,consumer_manifest_revision:1,confirmed:true});
 const r=f.op.health_rounds![0];acceptMember(r,0);await refresh(page);await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();
 acceptMember(r,1);r.state="complete";r.current=true;await refresh(page);await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeEnabled();
 await recordSubscription(page);await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toBeVisible();
 expect(f.writes[2].body).toMatchObject({round_id:r.id,consumer_manifest_id:manifest.id,consumer_manifest_revision:1,client_id:"client-a",expected_revision:0});expect(Object.values(f.state.snapshotReceipts)[0].expires_unix_nano).toBe(r.expires_unix_nano);expect(f.op.can_publish).toBe(false);
 await confirm(page,"Revoke consumer mapping");await expect(box.getByText("Consumer mapping revoked",{exact:true})).toBeVisible();await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toHaveCount(0);await expect(box.getByRole("button",{name:proveConsumer,exact:true})).toBeDisabled();
 expect(f.writes[3].body).toMatchObject({action:"revoke",expected_revision:1,members:[],confirmed:true});
});

test("N19 unconfirmed mapping survives reload with exact event, CAS revision and endpoints",async({page})=>{
 const f=await consumerFixture(page);await chooseConsumer(page);f.state.post="before";await confirm(page,recordConsumer);
 await expect(consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true})).toBeEnabled();const original=f.writes[0].raw;
 const stored=await page.evaluate(()=>Object.entries(sessionStorage).filter(([k])=>k.startsWith("dark-pro-consumer-attempt:")));expect(stored).toHaveLength(1);expect(stored[0][1]).not.toContain(csrf);expect(stored[0][1]).not.toContain("/api/v1/subscriptions/");
 await page.reload();await panel(page).getByRole("button",{name:"Open Node transfer",exact:true}).click();await panel(page).getByRole("button",{name:"View transfer",exact:true}).click();
 await expect(consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true})).toBeEnabled();expect(f.writes).toHaveLength(1);f.state.post="normal";
 await consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true}).click();await expect(consumerPanel(page).getByText("Recorded mapping matches the current destination",{exact:true})).toBeVisible();expect(f.writes[1].raw).toBe(original);
});

test("N19 stale mapping revision must be explicitly reloaded before another mutation",async({page})=>{
 const f=await consumerFixture(page,true),box=consumerPanel(page);await expect(box.getByRole("button",{name:recordConsumer,exact:true})).toBeEnabled();
 const head=f.state.consumer!;if(!head.enabled)throw Error("disabled fixture");head.revision=2;head.manifest={...head.manifest!,id:id(4002),event_id:id(3998),revision:2,consumer_manifest_sha:"2".padStart(64,"0")};
 await expect(box.getByText("The mapping revision changed. Reload its choices before recording another version.",{exact:true})).toBeVisible({timeout:5000});await expect(box.getByRole("button",{name:recordConsumer,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(0);
 await box.getByRole("button",{name:"Reload current endpoint choices",exact:true}).click();await confirm(page,recordConsumer);await expect.poll(()=>f.writes.length).toBe(1);expect(f.writes[0].body).toMatchObject({expected_revision:2});
});

test("N19 exact round retry retains its historical mapping across reload and head change",async({page})=>{
 const f=await consumerFixture(page,true);f.state.post="after";await confirm(page,proveConsumer);await expect(consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true})).toBeEnabled();const original=f.writes[0].raw,r=f.op.health_rounds![0],deadline=r.expires_unix_nano;
 const head=f.state.consumer!;if(!head.enabled)throw Error("disabled fixture");head.revision=2;head.manifest={...head.manifest!,id:id(4002),event_id:id(3998),revision:2,consumer_manifest_sha:"2".padStart(64,"0")};r.current=false;r.state="state_changed";
 await page.reload();await panel(page).getByRole("button",{name:"Open Node transfer",exact:true}).click();await panel(page).getByRole("button",{name:"View transfer",exact:true}).click();
 await expect(consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true})).toBeEnabled();expect(f.writes).toHaveLength(1);f.state.post="normal";await consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true}).click();
 await expect(consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true})).toHaveCount(0);expect(f.writes[1].raw).toBe(original);expect(r.expires_unix_nano).toBe(deadline);await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();
});

for(const [label,mutate] of [
 ["manifest event",(v:Record<string,unknown>)=>{v.event_id=id(9000);}],
 ["manifest revision",(v:Record<string,unknown>)=>{v.revision=30;}],
 ["manifest target",(v:Record<string,unknown>)=>{v.installation_id=id(9000);}]
] as const)test(`N19 mismatched ${label} reply remains unconfirmed`,async({page})=>{
 const f=await consumerFixture(page);await chooseConsumer(page);f.state.consumerResponse=v=>{mutate(v as Record<string,unknown>);return v;};await confirm(page,recordConsumer);
 await expect(consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true})).toBeEnabled();await expect(consumerPanel(page).getByText("Consumer mapping event recorded.",{exact:true})).toHaveCount(0);expect(f.writes).toHaveLength(1);
});

test("N19 mismatched round mapping and partial snapshot references remain unconfirmed",async({page})=>{
 const f=await consumerFixture(page,true);f.state.consumerResponse=v=>({...v as object,consumer_manifest_sha:"e".repeat(64)});await confirm(page,proveConsumer);
 await expect(consumerPanel(page).getByRole("button",{name:retryConsumer,exact:true})).toBeEnabled();await consumerPanel(page).getByRole("button",{name:"Discard local consumer retry",exact:true}).click();
 f.state.consumerResponse=null;completeConsumerRound(f);await refresh(page);await selectSubscriptionClient(page);f.state.snapshotResponse=v=>{delete v.consumer_manifest_revision;return v;};await recordSubscription(page);
 await expect(subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toHaveCount(0);
});

for(const [label,mutate] of [
 ["overflow timestamp",(r:HealthRound)=>{r.created_unix_nano=1e25;r.expires_unix_nano=1e25+8e9;}],
 ["more than eight seconds",(r:HealthRound)=>{r.expires_unix_nano=r.created_unix_nano+9e9;}],
 ["receipt at expiry",(r:HealthRound)=>{r.members[0].accepted_unix_nano=r.expires_unix_nano;}]
] as const)test(`N19 strict legacy round validation rejects ${label}`,async({page})=>{
 const f=await subscriptionFixture(page);await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeVisible();mutate(f.round);await refresh(page);
 await expect(panel(page).getByRole("alert")).toContainText("could not be verified");await expect(panel(page).getByText(roundCurrent,{exact:true})).toHaveCount(0);await expect(subscriptionPanel(page).getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(0);
});

test("N19 local rerenders cannot restart an unchanged server clock for mapping or output",async({page})=>{
 await page.addInitScript(()=>{const interval=window.setInterval,timeout=window.setTimeout;window.setInterval=((handler:TimerHandler,delay?:number,...args:unknown[])=>interval(handler,delay===500?60000:delay,...args)) as typeof window.setInterval;window.setTimeout=((handler:TimerHandler,delay?:number,...args:unknown[])=>timeout(handler,delay===3000?60000:delay,...args)) as typeof window.setTimeout;});
 const f=await consumerFixture(page,true);completeConsumerRound(f,2200);f.state.target.status.observed.lease_expires_unix_nano=(Date.now()+2400)*1e6;await refresh(page);await selectSubscriptionClient(page);
 const snapshot=subscriptionPanel(page),box=consumerPanel(page);await expect(snapshot.getByRole("button",{name:recordSnapshot,exact:true})).toBeEnabled();await expect(box.getByRole("button",{name:recordConsumer,exact:true})).toBeEnabled();
 const started=Date.now();let i=0;while(Date.now()-started<3000){await snapshot.getByRole("textbox",{name:"Find a synthetic client",exact:true}).fill(String(i));await box.getByRole("combobox",{name:"Consumer endpoint 7612",exact:true}).selectOption(i++%2?"11.0.0.2:28613":"11.0.0.2:28623");await new Promise(resolve=>setTimeout(resolve,150));}
 await expect(snapshot.getByRole("button",{name:recordSnapshot,exact:true})).toBeDisabled();await expect(box.getByRole("button",{name:recordConsumer,exact:true})).toBeDisabled();expect(f.writes).toHaveLength(0);
});

for(const consumer of [false,true])test(`N19 ${consumer?"consumer":"local"} snapshot accepts a client-assigned subset but rejects more routes than its full proof`,async({page})=>{
 const f=consumer?await consumerFixture(page,true):await subscriptionFixture(page);if(consumer){completeConsumerRound(f);await refresh(page);}await selectSubscriptionClient(page);
 f.state.snapshotResponse=v=>({...v,route_count:1});await recordSubscription(page);await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toBeVisible();
 f.state.snapshotResponse=v=>({...v,route_count:3});await recordSubscription(page);await expect(subscriptionPanel(page).getByRole("button",{name:retrySnapshot,exact:true})).toBeEnabled();await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toHaveCount(0);expect(f.writes).toHaveLength(2);
});

test("N19 confirmation crossing the original lease cannot send a consumer round",async({page})=>{
 const f=await consumerFixture(page,true);f.state.target.status.observed.lease_expires_unix_nano=(Date.now()+1400)*1e6;await refresh(page);await expect(consumerPanel(page).getByRole("button",{name:proveConsumer,exact:true})).toBeEnabled();
 page.once("dialog",async d=>{await new Promise(resolve=>setTimeout(resolve,1700));await d.accept();});await consumerPanel(page).getByRole("button",{name:proveConsumer,exact:true}).click();expect(f.writes).toHaveLength(0);
});

test("N19 delayed snapshot receipt is historical after its live round is invalidated",async({page})=>{
 const f=await consumerFixture(page,true),r=completeConsumerRound(f);await refresh(page);await selectSubscriptionClient(page);let release=()=>{};f.state.snapshotWait=new Promise<void>(resolve=>{release=resolve;});
 await recordSubscription(page);await expect.poll(()=>f.writes.length).toBe(1);r.current=false;r.state="state_changed";await refresh(page);release();
 await expect(subscriptionPanel(page).getByText("Snapshot recorded; current delivery not confirmed",{exact:true})).toBeVisible();await expect(subscriptionPanel(page).getByText(currentSnapshot,{exact:true})).toHaveCount(0);
});

test("N19 storage failure blocks writes and Persian consumer controls fit 320 pixels",async({page})=>{
 await page.setViewportSize({width:320,height:780});const f=await consumerFixture(page);await chooseConsumer(page);
 await page.evaluate(()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(key,value){if(key.startsWith("dark-pro-consumer-attempt:"))throw Error("quota");return original.call(this,key,value);};});await confirm(page,recordConsumer);
 await expect(consumerPanel(page).getByRole("alert")).toBeVisible();expect(f.writes).toHaveLength(0);await page.getByRole("button",{name:"Switch language",exact:true}).click();
 await expect(consumerPanel(page).getByRole("button",{name:"ثبت نگاشت مصرف‌کننده",exact:true})).toBeDisabled();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
