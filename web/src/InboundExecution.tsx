import { useEffect, useRef, useState } from "react";
import { AlertCircle, CheckCircle2, Clock3, ExternalLink, Play, RefreshCw, Server, ShieldCheck, Square, Users } from "lucide-react";
import { api, ApiError, useSession } from "./session";
import WorkspaceDialog from "./WorkspaceDialog";
import InboundReconciliation from "./InboundReconciliation";
import { navigateURL } from "./navigation";
import { networkNames, protocolNames } from "./inboundAuthoringSpec";
import { creationNodeName } from "./InboundCreationModel";
import type { Inbound, InboundNode } from "./inboundTypes";
import {
  executionActions, executionAPI, executionDisplayState, executionPath, maxExecutionClients,
  readExecutionAttempt, readExecutionClientPage, readExecutionOperation, readExecutionPreview, readExecutionState, sameExecutionClients, targetEvidence,
  type AccountingMode, type ExecutionAction, type ExecutionAttempt, type ExecutionBindings, type ExecutionBlocker,
  type ExecutionClientPage, type ExecutionCommand, type ExecutionInput, type ExecutionOperation, type ExecutionPreview, type ExecutionState, type ExecutionTarget,
} from "./inboundExecution";
import { executionActionLabel, executionBlockerLabel, executionStateLabel, executionWords } from "./inboundExecutionWords";
import "./InboundExecution.css";

type Tab = "overview" | "routes" | "clients" | "activity";
type Props = {
  inbound: Inbound; nodes: InboundNode[]; language: "fa" | "en"; tab: Tab; blocked: boolean; refreshKey: number; swapWorkspace: boolean;
  onOpenSetup: () => void; onSelectTab: (tab: Tab) => void; onChanged: () => void;
  onDirtyChange: (dirty: boolean) => void; onBusyChange: (busy: boolean) => void;
  onStateChange: (state: ExecutionState, observedAt: number) => void;
};
export default function InboundExecution(props: Props) {
  const { inbound, nodes, language, tab, blocked, refreshKey, swapWorkspace } = props;
  const t = executionWords[language], { session, expire } = useSession(), path = executionPath(inbound.id);
  const callbacks = useRef(props); callbacks.current = props;
  const [state, setState] = useState<ExecutionState | null>(null), [failed, setFailed] = useState(false), [unavailable, setUnavailable] = useState(false);
  const [refresh, setRefresh] = useState(0), [tick, setTick] = useState(performance.now()), [busy, setBusy] = useState(false), [notice, setNotice] = useState("");
  const [reviewAction, setReviewAction] = useState<ExecutionAction | null>(null), [review, setReview] = useState<ExecutionPreview | null>(null);
  const [selected, setSelected] = useState<string[]>([]), [accounting, setAccounting] = useState<AccountingMode>("per_client"), [selectionRevision, setSelectionRevision] = useState(0);
  const [selectionDirty, setSelectionDirty] = useState(false), selectionDirtyRef = useRef(false);
  const [reconciliationDirty, setReconciliationDirty] = useState(false), [reconciliationBusy, setReconciliationBusy] = useState(false);
  const [query, setQuery] = useState(""), [search, setSearch] = useState(""), [offset, setOffset] = useState(0), [clientPage, setClientPage] = useState<ExecutionClientPage | null>(null), [clientsFailed, setClientsFailed] = useState(false);
  const storageKey = `dark-pro-inbound-execution:${session?.user.id ?? "owner"}:${inbound.id}`;
  const [attempt, setAttempt] = useState<ExecutionAttempt | null>(() => {
    try { return readExecutionAttempt(sessionStorage.getItem(storageKey), inbound.id); } catch { return null; }
  });
  const attemptRef = useRef(attempt), busyRef = useRef(false), reviewInput = useRef<ExecutionInput | null>(null);
  const lifetime = useRef<AbortController | null>(null), received = useRef(0), serverClock = useRef({ at: 0, received: 0 });
  const [operation, setOperation] = useState<ExecutionOperation | null>(null);
  const fresh = received.current > 0 && tick - received.current < 15000 && !failed;
  const serverNow = serverClock.current.at ? serverClock.current.at + tick - serverClock.current.received : Date.now();
  const commandLocked = blocked || busy || reconciliationBusy || reconciliationDirty || !fresh || !!attempt || !state?.enabled;
  const locked = commandLocked || state?.desired_state === "archived";
  const selectionConflict = selectionDirty && !!state && selectionRevision !== state.revision;
  const visibleState = state ? executionDisplayState(state, serverNow, !fresh) : "unknown";
  const pendingChanges = !!state && (state.pending_changes === true || state.desired_revision !== state.active_revision || !sameExecutionClients(state.client_ids, state.effective_client_ids));

  function retain(value: ExecutionAttempt | null) {
    attemptRef.current = value; setAttempt(value);
    try { if (value) sessionStorage.setItem(storageKey, JSON.stringify(value)); else sessionStorage.removeItem(storageKey); } catch { /* Retained in memory when browser storage is unavailable. */ }
  }
  function setWorking(value: boolean) { busyRef.current = value; setBusy(value); }
  function resetSelection(next: ExecutionState) {
    setSelected([...next.client_ids]); setAccounting(next.accounting_mode); setSelectionRevision(next.revision);
    selectionDirtyRef.current = false; setSelectionDirty(false);
  }
  function accept(next: ExecutionState) {
    const pending = attemptRef.current;
    if (pending?.kind === "command" && next.latest_operation) {
      const input = JSON.parse(pending.body) as ExecutionCommand;
      if (next.latest_operation.event_id === input.event_id) readExecutionOperation(next.latest_operation, inbound.id, input);
    }
    const at = performance.now(); received.current = at; serverClock.current = { at: next.server_time_unix_milli ?? Date.now(), received: at };
    setState(next); setFailed(false); setUnavailable(false); setTick(at);
    if (!selectionDirtyRef.current) resetSelection(next);
    if (next.latest_operation) {
      setOperation(next.latest_operation);
      if (pending?.kind === "command") {
        const input = JSON.parse(pending.body) as ExecutionCommand;
        if (next.latest_operation.event_id === input.event_id) {
          retain(null); setNotice(t.accepted);
        }
      }
    }
    callbacks.current.onStateChange(next, Date.now());
  }
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    const timer = window.setInterval(() => setTick(performance.now()), 1000);
    const visible = () => setTick(performance.now()); document.addEventListener("visibilitychange", visible);
    return () => { controller.abort(); window.clearInterval(timer); document.removeEventListener("visibilitychange", visible); callbacks.current.onDirtyChange(false); callbacks.current.onBusyChange(false); };
  }, [inbound.id]);
  useEffect(() => { callbacks.current.onDirtyChange(selectionDirty || reviewAction !== null || reconciliationDirty); }, [selectionDirty, reviewAction, reconciliationDirty]);
  useEffect(() => { callbacks.current.onBusyChange(busy || reconciliationBusy); }, [busy, reconciliationBusy]);
  useEffect(() => {
    if (!session || session.user.role !== "owner") return;
    const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const next = readExecutionState(await executionAPI<unknown>(path, controller.signal), inbound.id);
        if (controller.signal.aborted) return;
        accept(next);
        const pending = attemptRef.current;
        if (pending?.kind === "command") {
          const input = JSON.parse(pending.body) as ExecutionCommand;
          try {
            const result = readExecutionOperation(await executionAPI<unknown>(`${path}/operations/${encodeURIComponent(input.event_id)}`, controller.signal), inbound.id, input);
            if (!controller.signal.aborted) { setOperation(result); retain(null); setNotice(t.accepted); }
          } catch (error) { if (error instanceof ApiError && error.status === 401) expire(); /* A missing receipt does not prove no execution. */ }
        }
      } catch (error) {
        if (controller.signal.aborted) return;
        setFailed(true);
        if (error instanceof ApiError && error.status === 404) setUnavailable(true);
        if (error instanceof ApiError && error.status === 401) expire();
      } finally { if (!controller.signal.aborted) timer = setTimeout(() => { void poll(); }, document.hidden ? 10000 : 4000); }
    }
    void poll(); return () => { controller.abort(); clearTimeout(timer); };
  }, [inbound.id, refresh, refreshKey, session, expire]);
  useEffect(() => {
    if (!review || !state) return;
    if (review.state.revision !== state.revision || review.state.plan_revision !== state.plan_revision || review.state.inbound_revision !== state.inbound_revision) {
      setReview(null); reviewInput.current = null; setNotice(t.reviewChanged);
    }
  }, [state?.revision, state?.plan_revision, state?.inbound_revision]);
  useEffect(() => {
    if (tab !== "clients" || !session || session.user.role !== "owner") return;
    const controller = new AbortController(); setClientsFailed(false); setClientPage(null);
    const params = new URLSearchParams({ limit: "25", offset: String(offset), q: search });
    void api<unknown>("/api/v1/workspace/clients?" + params, { signal: controller.signal }).then(value => {
      const next = readExecutionClientPage(value, offset); if (!controller.signal.aborted) setClientPage(next);
    }).catch(error => { if (!controller.signal.aborted) { setClientsFailed(true); if (error instanceof ApiError && error.status === 401) expire(); } });
    return () => controller.abort();
  }, [tab, search, offset, refresh, session, expire]);

  async function beginReview(action: ExecutionAction) {
    const controller = lifetime.current;
    if (!controller || controller.signal.aborted || busyRef.current || commandLocked || !state || selectionDirty || !state.allowed_actions.includes(action)) return;
    setReviewAction(action); setReview(null); setNotice(""); setWorking(true);
    const input: ExecutionInput = { event_id: crypto.randomUUID(), action, expected_revision: state.revision, expected_plan_revision: state.plan_revision,
      expected_inbound_revision: state.inbound_revision, client_ids: [...state.client_ids].sort(), accounting_mode: state.accounting_mode,
      reason: `${executionActionLabel(action, language)} ${inbound.name}`.slice(0, 200) };
    reviewInput.current = input;
    try {
      const result = readExecutionPreview(await executionAPI<unknown>(path + "/preview", controller.signal, JSON.stringify(input), session?.csrf_token), inbound.id, input);
      if (!controller.signal.aborted) setReview(result);
    } catch (error) {
      if (!controller.signal.aborted) { setNotice(t.actionFailed); if (error instanceof ApiError && error.status === 401) expire(); }
    } finally { if (!controller.signal.aborted) setWorking(false); }
  }
  async function send(candidate: ExecutionAttempt) {
    const controller = lifetime.current;
    if (!controller || controller.signal.aborted || busyRef.current) return;
    retain(candidate); setWorking(true); setNotice("");
    try {
      const value = await executionAPI<unknown>(path + (candidate.kind === "command" ? "/commands" : "/clients"), controller.signal, candidate.body, session?.csrf_token);
      if (controller.signal.aborted) return;
      if (candidate.kind === "command") {
        setOperation(readExecutionOperation(value, inbound.id, JSON.parse(candidate.body) as ExecutionCommand)); setNotice(t.accepted);
      } else {
        const input = JSON.parse(candidate.body) as ExecutionBindings, next = readExecutionState(value, inbound.id);
        if (next.revision < input.expected_revision + 1 || !sameExecutionClients(next.client_ids, input.client_ids) || next.accounting_mode !== input.accounting_mode) throw Error("mismatched_execution_bindings");
        resetSelection(next); accept(next); setNotice(t.clientsSaved);
      }
      retain(null); setReviewAction(null); setReview(null); reviewInput.current = null;
      setRefresh(value => value + 1); callbacks.current.onChanged();
    } catch (error) {
      if (controller.signal.aborted) return;
      if (error instanceof ApiError && error.status >= 400 && error.status < 500 && ![408, 429].includes(error.status)) {
        retain(null); setReviewAction(null); setReview(null); reviewInput.current = null; setNotice(t.actionFailed); setRefresh(value => value + 1);
        if (error.status === 401) expire();
      } else { setNotice(attemptRef.current ? t.unknownResult : t.accepted); setReviewAction(null); setReview(null); setRefresh(value => value + 1); }
    } finally { if (!controller.signal.aborted) setWorking(false); }
  }
  function confirm() {
    const input = reviewInput.current;
    if (!review?.valid || !input || !state || commandLocked || !state.allowed_actions.includes(input.action) || state.revision !== input.expected_revision || state.plan_revision !== input.expected_plan_revision || state.inbound_revision !== input.expected_inbound_revision) return;
    void send({ inbound_id: inbound.id, kind: "command", body: JSON.stringify({ ...input, review_sha: review.review_sha }) });
  }
  function saveClients() {
    if (!state || locked || !selectionDirty || selectionConflict) return;
    const input: ExecutionBindings = { event_id: crypto.randomUUID(), expected_revision: state.revision, client_ids: [...selected].sort(), accounting_mode: accounting,
      reason: language === "fa" ? "تغییر انتخاب کلاینت‌های اینباند" : "Update Inbound client selection" };
    void send({ inbound_id: inbound.id, kind: "clients", body: JSON.stringify(input) });
  }
  function changeSelection(ids: string[], mode = accounting) {
    setSelected(ids); setAccounting(mode); setNotice("");
    const changed = !!state && (!sameExecutionClients(ids, state.client_ids) || mode !== state.accounting_mode);
    selectionDirtyRef.current = changed; setSelectionDirty(changed);
  }
  function openSwap() {
    if (navigateURL(`/dark-swap?inbound=${inbound.id}&tab=routes`)) window.dispatchEvent(new PopStateEvent("popstate", { state: history.state }));
  }
  const locale = language === "fa" ? "fa-IR" : "en-US";
  const number = (value: number) => value.toLocaleString(locale);
  const date = (nano?: number) => nano ? new Date(nano / 1e6).toLocaleString(locale) : t.unknown;
  const stateLabel = (value: string | undefined) => executionStateLabel(value || "unknown", language);
  const blockers = (items: ExecutionBlocker[]) => items.length > 0 && <div className="managed-blockers" role="status"><h4><AlertCircle size={17}/>{t.blockers}</h4><ul>{items.slice(0, 100).map((item, index) => <li key={`${item.code}:${item.node_id}:${item.client_id}:${index}`}><span>{item.node_id && <strong>{creationNodeName(nodes, item.node_id)}: </strong>}{item.client_id && <bdi>{item.client_id}: </bdi>}{executionBlockerLabel(item, language)}</span><details><summary>{t.diagnostics}</summary><code>{item.code}{item.field ? ` · ${item.field}` : ""}</code></details></li>)}</ul>{items.length > 100 && <p>{number(items.length - 100)} {t.more}</p>}</div>;
  const targetCards = (targets: ExecutionTarget[], reviewMode = false) => <div className="managed-targets">{targets.map(target => {
    const evidence = targetEvidence(target, serverNow), targetNode = nodes.find(node => node.node_id === target.node_id), own = target.listeners.filter(listener => listener.inbound_id === inbound.id);
    return <article className="managed-target" key={target.node_id} data-execution-target={target.node_id}>
      <header><span className="managed-target-icon"><Server size={18}/></span><div><strong>{creationNodeName(nodes, target.node_id)}</strong><small>{targetNode?.kind === "hub" || target.node_id === "hub-local" ? t.hub : t.node}</small></div>{!reviewMode && <span className={"managed-status managed-status-" + (fresh ? evidence : "stale")}>{stateLabel(fresh ? evidence === "ready" ? "active" : evidence === "pending" ? "verifying" : evidence : "stale")}</span>}</header>
      {own.length > 0 && <div className="managed-listeners">{own.map(listener => <div key={listener.tag}><span>{listener.kind.includes("swap") ? t.swap : listener.kind === "tunnel" ? t.tunnel : t.direct}</span><bdi dir="ltr">{listener.listen}{listener.port ? `:${listener.port}` : ""}</bdi><small><bdi>{listener.protocol} · {listener.network}</bdi></small></div>)}</div>}
      {!reviewMode && <dl className="managed-evidence"><div><dt>{t.process}</dt><dd>{target.process_ready === undefined ? t.unknown : target.process_ready ? target.health_state === "engine_ready" ? t.engineReady : t.yes : t.no}</dd></div><div><dt>{t.connectivity}</dt><dd>{target.health_state === "engine_ready" ? t.unknown : stateLabel(target.health_state)}</dd></div><div><dt>{t.accounting}</dt><dd>{stateLabel(target.accounting_state)}</dd></div><div><dt>{t.lease}</dt><dd>{target.lease_expires_unix_nano ? target.lease_expires_unix_nano / 1e6 <= serverNow ? t.expired : date(target.lease_expires_unix_nano) : t.unknown}</dd></div></dl>}
      {blockers(target.blockers)}
      <details className="managed-diagnostics"><summary>{t.diagnostics}</summary><dl><div><dt>{t.generation}</dt><dd><bdi>{target.desired_generation} / {target.observed_generation}</bdi></dd></div><div><dt>{t.lastReport}</dt><dd>{date(target.reported_at_unix_nano)}</dd></div><div><dt>Installation</dt><dd><bdi>{target.installation_id}</bdi></dd></div>{target.config_sha && <div><dt>Configuration SHA</dt><dd><bdi>{target.config_sha}</bdi></dd></div>}</dl>{reviewMode && target.listeners.length > own.length && <p>{t.siblingHelp}</p>}</details>
    </article>;
  })}</div>;
  const operationCard = operation && <section className="managed-operation" aria-label={t.operation}><div className="managed-section-heading"><h3><Clock3 size={18}/>{t.operation}</h3><span className={"managed-status managed-status-" + operation.state}>{stateLabel(operation.state === "active" ? "accepted" : operation.state)}</span></div><p>{executionActionLabel(operation.action, language)} · {date(operation.created_unix_nano)}</p>{blockers(operation.blockers)}<details><summary>{t.diagnostics}</summary><p><bdi>{operation.id}</bdi></p><p>{t.draft}: {number(operation.revision)}</p><ul>{operation.targets.map(target => <li key={target.node_id}>{creationNodeName(nodes, target.node_id)} · {stateLabel(target.state === "active" ? "accepted" : target.state)}</li>)}</ul></details></section>;
  const statusNotice = <>{notice && <p className="managed-notice" role="status">{notice}</p>}{attempt && <section className="managed-uncertain" role="alert"><h3>{t.pendingRequest}</h3><p>{t.unknownResult}</p><div className="native-actions"><button type="button" disabled={busy} onClick={() => setRefresh(value => value + 1)}><RefreshCw size={16}/>{t.checkResult}</button><button type="button" disabled={busy || !fresh || blocked} onClick={() => void send(attempt)}>{t.retryExact}</button></div><details><summary>{t.diagnostics}</summary><bdi>{(JSON.parse(attempt.body) as ExecutionBindings).event_id}</bdi></details></section>}</>;

  return <div className="managed-inbound" data-managed-inbound={inbound.id} aria-busy={busy || reconciliationBusy}>
    {!state && !failed && <p role="status">{t.loading}</p>}
    {(failed || !state?.enabled && state) && <div className="managed-unavailable" role="alert"><AlertCircle size={20}/><p>{unavailable || state?.enabled === false ? t.unavailable : t.failedLoad}</p><button type="button" disabled={busy} onClick={() => setRefresh(value => value + 1)}>{t.refresh}</button></div>}
    {state && <>
      {!fresh && !failed && <p className="native-warning" role="status">{t.stale}</p>}
      {statusNotice}
      <section hidden={tab !== "overview"} role="tabpanel" id="inbound-panel-overview" aria-labelledby="inbound-tab-overview" className="inbound-detail-panel">
        <section className="native-card managed-overview" aria-label={t.title}>
          <div className="managed-section-heading"><div><h3><ShieldCheck size={20}/>{t.title}</h3><p><bdi>{inbound.spec ? `${protocolNames[inbound.spec.protocol]} · ${networkNames[inbound.spec.network] ?? inbound.spec.network} / ${inbound.spec.security.toUpperCase()}` : inbound.transport}</bdi></p></div><span className={"managed-status managed-status-" + visibleState} data-managed-state={visibleState}>{stateLabel(visibleState)}</span></div>
          <dl className="managed-summary"><div><dt>{t.draft}</dt><dd>{number(state.plan_revision)}</dd></div><div><dt>{t.effective}</dt><dd>{state.active_revision ? number(state.active_plan_revision ?? state.active_revision) : "—"}</dd></div><div><dt>{t.desiredClients}</dt><dd>{number(state.client_ids.length)}</dd></div><div><dt>{t.effectiveClients}</dt><dd>{number(state.effective_client_ids.length)}</dd></div></dl>
          {pendingChanges && <div className="managed-pending"><span>{t.pending}</span><p>{t.pendingHelp}</p></div>}
          <div className="native-actions managed-actions">{executionActions.filter(action => state.allowed_actions.includes(action)).map(action => <button key={action} type="button" className={action === "activate" || action === "apply" ? "native-primary" : action === "stop" || action === "archive" ? "managed-restrictive" : ""} disabled={commandLocked || selectionDirty} onClick={() => void beginReview(action)}>{action === "activate" ? <Play size={16}/> : action === "stop" ? <Square size={15}/> : action === "restart" ? <RefreshCw size={16}/> : null}{executionActionLabel(action, language)}</button>)}<button type="button" disabled={busy || reconciliationBusy || reconciliationDirty || !!attempt || selectionDirty || state.desired_state === "archived" || blocked} onClick={() => callbacks.current.onOpenSetup()}>{t.edit}</button></div>
          {selectionDirty && <p className="native-warning">{t.selectionHelp}</p>}
          {blockers(state.blockers)}
          <InboundReconciliation inboundID={inbound.id} nodes={nodes} language={language} entries={state.reconciliations ?? []}
            disabled={blocked || busy || !!attempt || selectionDirty || reviewAction !== null || !fresh || !state.enabled}
            onChanged={() => { setRefresh(value => value + 1); callbacks.current.onChanged(); }} onBusyChange={setReconciliationBusy} onDirtyChange={setReconciliationDirty}/>
          <div className="managed-section-heading"><h4>{t.targets}</h4><button type="button" className="native-text-action" disabled={busy} onClick={() => setRefresh(value => value + 1)}><RefreshCw size={15}/>{t.refresh}</button></div>
          {state.targets.length ? targetCards(state.targets) : <p>{t.noTargets}</p>}
          <div className="managed-publication"><span>{t.publication}</span><strong>{state.publication_state ? stateLabel(state.publication_state) : t.publicationUnknown}</strong></div>
          <details className="managed-diagnostics"><summary>{t.diagnostics}</summary><dl><div><dt>{t.desired}</dt><dd>{stateLabel(state.desired_state === "active" ? "accepted" : state.desired_state)}</dd></div><div><dt>{t.observed}</dt><dd>{stateLabel(state.observed_state === "active" ? "accepted" : state.observed_state)}</dd></div><div><dt>{t.accountingLabel}</dt><dd>{state.accounting_mode === "per_client" ? t.perClient : t.ownerAggregate}</dd></div></dl></details>
        </section>
        {operationCard}
      </section>
      <section hidden={tab !== "routes"} role="tabpanel" id="inbound-panel-routes" aria-labelledby="inbound-tab-routes" className="inbound-detail-panel"><section className="native-card managed-routes"><div className="managed-section-heading"><h3>{t.routes}</h3>{swapWorkspace ? <button type="button" disabled={locked} onClick={() => callbacks.current.onOpenSetup()}>{t.editPaths}</button> : <button type="button" disabled={busy} onClick={openSwap}><ExternalLink size={16}/>{t.openSwap}</button>}</div><p>{t.routesHelp}</p>{state.targets.length ? targetCards(state.targets) : <p>{t.noRoutes}</p>}</section></section>
      <section hidden={tab !== "clients"} role="tabpanel" id="inbound-panel-clients" aria-labelledby="inbound-tab-clients" className="inbound-detail-panel"><section className="native-card managed-clients" aria-label={t.clients}>
        <h3><Users size={19}/>{t.clients}</h3><p>{t.selectionHelp}</p><dl className="managed-summary"><div><dt>{t.desiredClients}</dt><dd>{number(selected.length)}</dd></div><div><dt>{t.effectiveClients}</dt><dd>{number(state.effective_client_ids.length)}</dd></div></dl>
        <form className="managed-client-search" onSubmit={event => { event.preventDefault(); setSearch(query.trim()); setOffset(0); }}><label>{t.search}<input value={query} maxLength={128} onChange={event => setQuery(event.target.value)}/></label><button type="submit">{t.searchButton}</button></form>
        {clientsFailed && <p className="native-warning" role="alert">{t.clientsFailed}</p>}{!clientPage && !clientsFailed && tab === "clients" && <p role="status">{t.loading}</p>}
        <div className="managed-client-list">{clientPage?.clients.map(client => {
          const chosen = selected.includes(client.id), effective = state.effective_client_ids.includes(client.id), clientProblems = state.blockers.filter(item => item.client_id === client.id);
          const problem = client.archived ? t.clientArchived : client.deployment_state === "disabled" ? t.clientDisabled : client.expires_unix > 0 && client.expires_unix * 1000 <= serverNow ? t.clientExpired : clientProblems.length ? executionBlockerLabel(clientProblems[0], language) : "";
          return <label className="managed-client-row" key={client.id}><input type="checkbox" aria-label={`${t.selected}: ${client.id}`} checked={chosen} disabled={locked || !!problem && !chosen || !chosen && selected.length >= maxExecutionClients} onChange={event => changeSelection(event.target.checked ? [...selected, client.id] : selected.filter(id => id !== client.id))}/><span><strong>{client.name}</strong><bdi>{client.id}</bdi><span className="managed-client-tags">{effective && <small><CheckCircle2 size={13}/>{t.effectiveClient}</small>}{effective && !chosen && <small>{t.pendingRemoval}</small>}{problem && <small className="managed-client-ineligible">{problem}</small>}</span></span></label>;
        })}{clientPage?.clients.length === 0 && <p>{t.emptyClients}</p>}</div>
        <div className="managed-client-pagination"><button type="button" disabled={offset === 0 || busy} onClick={() => setOffset(value => Math.max(0, value - 25))}>{t.previous}</button><button type="button" disabled={!clientPage?.has_more || busy} onClick={() => setOffset(value => value + 25)}>{t.next}</button></div>
        <div className="managed-accounting"><label>{t.accountingLabel}<select value={accounting} disabled={locked || (state.allowed_accounting_modes?.length ?? 0) < 2} onChange={event => changeSelection(selected, event.target.value as AccountingMode)}>{(state.allowed_accounting_modes?.length ? state.allowed_accounting_modes : [state.accounting_mode]).map(mode => <option key={mode} value={mode}>{mode === "per_client" ? t.perClient : t.ownerAggregate}</option>)}</select></label>{accounting === "owner_aggregate" && <p>{t.aggregateHelp}</p>}</div>
        {selectionConflict && <p className="native-warning" role="alert">{t.selectionChanged}</p>}
        <div className="native-actions"><button type="button" className="native-primary" disabled={locked || !selectionDirty || selectionConflict} onClick={saveClients}>{t.saveClients}</button><button type="button" disabled={busy || !!attempt || !selectionDirty} onClick={() => resetSelection(state)}>{t.resetClients}</button><button type="button" disabled={busy} onClick={() => callbacks.current.onSelectTab("overview")}>{t.returnOverview}</button></div>
        {(selected.length > 0 || state.effective_client_ids.length > 0) && <details className="managed-diagnostics"><summary>{t.desiredClients} / {t.effectiveClients}</summary><div className="managed-client-ids">{[...new Set([...selected, ...state.effective_client_ids])].slice(0, 100).map(id => <span key={id}><bdi>{id}</bdi><small>{selected.includes(id) ? t.selected : t.pendingRemoval}{state.effective_client_ids.includes(id) ? ` · ${t.effectiveClient}` : ""}</small></span>)}</div>{Math.max(selected.length, state.effective_client_ids.length) > 100 && <p>{t.more}: {number(Math.max(selected.length, state.effective_client_ids.length) - 100)}</p>}</details>}
      </section></section>
      <section hidden={tab !== "activity"} role="tabpanel" id="inbound-panel-activity" aria-labelledby="inbound-tab-activity" className="inbound-detail-panel"><div className="native-card"><h3>{t.activity}</h3><p>{t.auditHelp}</p>{operationCard || <p>{t.noOperation}</p>}</div></section>
    </>}
    {reviewAction && <WorkspaceDialog open title={`${t.review} · ${executionActionLabel(reviewAction, language)}`} closeLabel={t.cancel} busy={busy} onClose={() => { setReviewAction(null); setReview(null); reviewInput.current = null; }}>
      <div className="managed-review"><p>{t.actionHelp[reviewAction]}</p>{busy && !review && <p role="status">{t.reviewing}</p>}{review && <><dl className="managed-summary"><div><dt>{t.draft}</dt><dd>{number(review.state.plan_revision)}</dd></div><div><dt>{t.reviewClients}</dt><dd>{number(review.state.client_ids.length)}</dd></div></dl>{blockers(review.blockers)}<h3>{t.reviewTargets}</h3>{review.targets.length ? targetCards(review.targets, true) : <p>{t.reviewEmpty}</p>}</>}{notice && <p role="alert">{notice}</p>}<div className="native-actions"><button type="button" className="native-primary" disabled={busy || commandLocked || !review?.valid || !reviewInput.current} onClick={confirm}>{t.confirm}</button><button type="button" disabled={busy} onClick={() => { setReviewAction(null); setReview(null); reviewInput.current = null; }}>{t.cancel}</button></div></div>
    </WorkspaceDialog>}
  </div>;
}
