import { Component, Suspense, type ReactNode } from "react";
import type { Language } from "./session";

type Props = { language: Language; children: ReactNode };

class LoadErrorBoundary extends Component<Props, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() {
    if (this.state.failed) return <div className="empty-panel" role="alert">
      <p>{this.props.language === "fa" ? "این بخش بارگذاری نشد. اتصال را بررسی کنید و صفحه را دوباره بارگذاری کنید." : "This page could not load. Check your connection and reload the page."}</p>
      <button type="button" onClick={() => window.location.reload()}>{this.props.language === "fa" ? "بارگذاری دوبارهٔ صفحه" : "Reload page"}</button>
    </div>;
    return this.props.children;
  }
}

export default function PageLoadBoundary({ language, children }: Props) {
  return <LoadErrorBoundary language={language}>
    <Suspense fallback={<div className="empty-panel" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" />
      <p>{language === "fa" ? "در حال بارگذاری این بخش…" : "Loading this page…"}</p>
    </div>}>{children}</Suspense>
  </LoadErrorBoundary>;
}
