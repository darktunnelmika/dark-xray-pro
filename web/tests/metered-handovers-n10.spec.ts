import { expect, test, type Page as BrowserPage } from "@playwright/test";
import type { Channel, Command, CommandRequest, MeteredCondition, Observation, Scope } from "../src/meteredRouteTransactions";
import type { Handover, Plan } from "../src/meteredHandovers";

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
 const state={scope,source,target,ops:[] as Handover[],post:"normal" as "normal"|"before"|"after",readError:false,fresh:true,malformed:false};
 const writes:{path:string;raw:string;body:unknown;csrf:string}[]=[],reads:string[]=[];
 await page.route("**/api/v1/**",async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;
  if(path.endsWith("/session/status"))return route.fulfill({json:{authenticated:true,user:{id:role,username:"qa",role,representative_id:role},csrf_token:csrf,expires_unix:2000000000}});
  if(path==="/api/v1/fleet")return route.fulfill({json:{nodes:[],total_nodes:0,online_nodes:0,offline_nodes:0,as_of:new Date().toISOString()}});
  if(req.method()==="GET"){
   reads.push(path);
   if(path==="/api/v1/workspace/nodes")return route.fulfill({json:{mode:"new_pro_lab_only",nodes:[],agent_transport_scope:"loopback_only"}});
   if(path==="/api/v1/workspace/metered-route-transactions")return route.fulfill({json:state.scope});
   if(path===base)return route.fulfill({json:{operations:state.ops,next_cursor:""}});
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
  }else{
   const input=body as CommandRequest,op=state.ops.find(o=>o.id===path.split("/").at(-2));
   const v=input.action==="drain"?source:target;
   let c=v.commands.find(c=>c.id===input.id);
   if(!c){c={...row(v.node_id,v.installation_id,v.status.serial+1,input.action,input.action==="stage"?input.id:input.stage_id),id:input.id};v.commands.unshift(c);}
   if(op&&input.action==="drain"){op.drain_command_id=c.id;op.drain_command=c;op.phase="source_drain_pending";}
   if(op&&input.action==="apply"){op.apply_command_id=c.id;op.apply_command=c;op.phase="target_apply_pending";}
   reply=c;
  }
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

async function healthFixture(page:BrowserPage){
 const f=await fixture(page);f.state.scope.health_enabled=true;
 const src=f.state.source,dst=f.state.target,usage=structuredClone(src.status.usage!);
 src.status.observed={...src.status.observed,process_ready:false,active_sequence:0,active_sha:"",lease_expires_unix_nano:0};
 src.status.usage={...usage,settlement:{epoch_id:usage.epoch_id!,sequence:"2",sha256:sha}};
 dst.status.current_stage_id=id(21);delete dst.status.staged_id;dst.status.serial=32;dst.status.usage={epoch_id:id(42),epoch_sha:sha,admission_sha:sha,settlement:zero()};
 dst.status.observed={...src.status.observed,highwater:1,saved_sequence:1,saved_sha:sha,active_sequence:1,active_sha:sha,process_ready:true,lease_expires_unix_nano:(Date.now()+60000)*1e6};
 const c=receipt(row(dst.node_id,dst.installation_id,32,"apply",id(21)),"applied",dst.status.observed,dst.status.usage);
 dst.commands=[c];dst.controls={stage:false};
 const op:Handover={id:id(88),source_node:src.node_id,target_node:dst.node_id,source_installation:src.installation_id,target_installation:dst.installation_id,source_stage_id:id(11),target_stage_id:id(21),source_usage:usage,created_unix_nano:Date.now()*1e6,drain_command_id:"",apply_command_id:c.id,apply_command:c,cancelled:false,can_publish:false,phase:"target_applied_health_required"};
 f.state.ops=[op];await open(page);await panel(page).getByRole("button",{name:"View transfer",exact:true}).click();
 await panel(page).getByRole("combobox",{name:"Inbound to test",exact:true}).selectOption("7612");return {...f,op};
}

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
 expect(f.writes.every(w=>w.csrf===csrf)).toBe(true);await expect(panel(page).getByText(/Protocol health, publication and cross-Node rollback are pending/)).toBeVisible();
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
