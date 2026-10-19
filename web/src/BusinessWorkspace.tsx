import type { Language } from "./session";
import type { Section } from "./business/shared";
import { useBusinessWorkspace } from "./business/useBusinessWorkspace";
import Header from "./business/Header";
import Clients from "./business/Clients";
import Groups from "./business/Groups";
import Representatives from "./business/Representatives";
import Finance from "./business/Finance";
import Marketplace from "./business/Marketplace";
import Account from "./business/Account";
const views = { clients: Clients, groups: Groups, representatives: Representatives, marketplace: Marketplace, finance: Finance, account: Account };
export default function BusinessWorkspace({ language, section }: { language: Language; section: Section }) {
  const model = useBusinessWorkspace(language, section);
  const View = views[section];
  return <section className="business-workspace" aria-label="Business workspace">
    <Header model={model}/>
    <View model={model}/>
    {model.notice && <div className="business-notice" role="status">{model.notice}</div>}
    <p className="business-footnote">{model.t.unsafe}</p>
  </section>;
}
