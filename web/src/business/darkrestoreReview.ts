export type RestoreFinding = { code: string; count: number };
export type RestoreGroup = { source_group_id: string; name: string; count: number; archived: number; promoted: number; historical_bytes: string };
export type RestoreClient = { source_id: string; source_group_id: string; label: string; uuid?: string; quota_kind: "volume" | "legacy_unlimited"; quota_bytes: string; historical_bytes: string; expiry_unix: number; status: "requires_manual_review" | "archive_history_only" | "skip_already_promoted"; review_reasons: string[] };
export type RestoreReview = {
  version: 1; mode: "offline_operator_review_only"; source_sha256: string; source_size_bytes: number;
  as_of_unix: number; status: "ready_for_review"; import_enabled: false;
  summary: { status: "ready_for_manual_review"; subscriptions: number; groups: number; total_consumed_bytes: string; findings: RestoreFinding[] };
  groups: RestoreGroup[]; clients: RestoreClient[];
};
const obj = (v: unknown): v is Record<string, unknown> => v!==null && typeof v==="object" && !Array.isArray(v);
const safeInt = (v: unknown, min=0): v is number => typeof v==="number" && Number.isSafeInteger(v) && v>=min;
const bytes = (v: unknown): v is string => typeof v==="string" && /^(0|[1-9][0-9]{0,29})$/.test(v);
const sha = (v: unknown): v is string => typeof v==="string" && /^[a-f0-9]{64}$/.test(v);
const uuid = (v: unknown): v is string => typeof v==="string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(v);
const label = (v: unknown): v is string => typeof v==="string" && v.length>0 && v.length<=1024 && [...v].length<=128 && !/[\u0000-\u001f\u007f]/.test(v);
const reason = (v: unknown): v is string => typeof v==="string" && /^[a-z][a-z0-9_]{0,127}$/.test(v);
export function parseRestoreReview(v: unknown): RestoreReview {
  if (!obj(v) || v.version!==1 || v.mode!=="offline_operator_review_only" || v.status!=="ready_for_review" ||
      v.import_enabled!==false || !sha(v.source_sha256) || !safeInt(v.source_size_bytes,16) ||
      !safeInt(v.as_of_unix,1) || !obj(v.summary) || v.summary.status!=="ready_for_manual_review" ||
      !safeInt(v.summary.subscriptions) || !safeInt(v.summary.groups) || !bytes(v.summary.total_consumed_bytes) ||
      !Array.isArray(v.summary.findings) || v.summary.findings.length!==0 ||
      !Array.isArray(v.groups) || !Array.isArray(v.clients) || v.clients.length>50000 ||
      v.clients.length!==v.summary.subscriptions || v.groups.length!==v.summary.groups) throw Error("invalid_restore_review");
  const groupCounts=new Map<string,{count:number;archived:number;promoted:number;used:bigint}>();
  let previousGroup="";
  for(const g of v.groups){
    if(!obj(g) || !label(g.source_group_id) || !label(g.name) || !safeInt(g.count) || !safeInt(g.archived) ||
       !safeInt(g.promoted) || !bytes(g.historical_bytes) || g.count<g.archived || g.count<g.promoted ||
       previousGroup>=g.source_group_id) throw Error("invalid_restore_group");
    groupCounts.set(g.source_group_id,{count:0,archived:0,promoted:0,used:0n});
    previousGroup=g.source_group_id;
  }
  let previousClient="";
  for(const c of v.clients){
    if(!obj(c) || !label(c.source_id) || !label(c.label) || !label(c.source_group_id) ||
       !groupCounts.has(c.source_group_id) || previousClient>=c.source_id ||
       !(c.uuid===undefined || uuid(c.uuid)) || !bytes(c.quota_bytes) || !bytes(c.historical_bytes) ||
       !safeInt(c.expiry_unix) || !["volume","legacy_unlimited"].includes(String(c.quota_kind)) ||
       (c.quota_kind==="legacy_unlimited") !== (c.quota_bytes==="0") ||
       !["requires_manual_review","archive_history_only","skip_already_promoted"].includes(String(c.status)) ||
       !Array.isArray(c.review_reasons) || c.review_reasons.length>32 || !c.review_reasons.every(reason))
      throw Error("invalid_restore_client");
    previousClient=c.source_id;
    const g=groupCounts.get(c.source_group_id)!;
    g.count++;
    if(c.status==="archive_history_only"||(c.status==="skip_already_promoted"&&c.review_reasons.includes("archived_history_must_not_be_recreated_active")))g.archived++;
    if(c.status==="skip_already_promoted")g.promoted++;
    g.used+=BigInt(c.historical_bytes);
  }
  let all=0n;
  for(const g of v.groups){
    const sum=groupCounts.get(g.source_group_id)!;
    if(sum.count!==g.count || sum.archived!==g.archived || sum.promoted!==g.promoted ||
       sum.used!==BigInt(g.historical_bytes)) throw Error("inconsistent_restore_group");
    all+=sum.used;
  }
  if(all!==BigInt(v.summary.total_consumed_bytes))throw Error("inconsistent_restore_total");
  return v as RestoreReview;
}
export type RestoreMapping = { source_group_id: string; target_representative_id: string };
export function createReviewMapping(review: RestoreReview, selections: Record<string,string>, validRepresentativeIDs: string[]): {version:1; source_sha256:string; as_of_unix:number; import_enabled:false; groups: RestoreMapping[]} {
  const eligible=new Set(validRepresentativeIDs);
  const groups=review.groups.map(g=>{
    const target=selections[g.source_group_id] ?? "";
    if(!eligible.has(target))throw Error("missing_target_representative");
    return {source_group_id:g.source_group_id,target_representative_id:target};
  });
  return {version:1,source_sha256:review.source_sha256,as_of_unix:review.as_of_unix,import_enabled:false,groups};
}
