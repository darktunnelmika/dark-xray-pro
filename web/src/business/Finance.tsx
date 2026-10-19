import type { BusinessModel } from "./useBusinessWorkspace";
import { CreditCard } from "lucide-react";
import { gb } from "./shared";
import { businessGranted } from "./permissionsV9";
import RepresentativeSelfActivityV9 from "./RepresentativeSelfActivityV9";
export default function Finance({ model }: { model: BusinessModel }) {
  const { t, events, eventOffset, setEventOffset } = model;
  if(!businessGranted(model.session.user,"view_finance")){
    return <section className="business-panel" role="status">{model.language==="fa"?
      "اجازهٔ مشاهدهٔ گزارش‌های مالی برای این حساب فعال نیست.":
      "Financial reports are disabled for this representative account."}</section>;
  }
  return <>
      {model.session.user.role==="representative"&&<RepresentativeSelfActivityV9 model={model}/>}
      <div className="business-panel">
        <h3><CreditCard size={17}/>{t.history}</h3>
        {events && events.length ? <div className="business-records">{events.map((e) => <div key={e.event_id}>
          <strong>{e.kind} · {e.event_id}</strong><span>{e.reason} · {gb(e.volume_delta_bytes)} · {e.unlimited_delta_units} {t.unlimited}</span>
        </div>)}</div> : <p>{events ? t.empty : t.loading}</p>}
        <div className="business-pagination"><button disabled={eventOffset === 0} onClick={() => setEventOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
          <button disabled={!events || events.length < 50} onClick={() => setEventOffset((x) => x + 50)}>{t.next}</button></div>
      </div>
  </>;
}
