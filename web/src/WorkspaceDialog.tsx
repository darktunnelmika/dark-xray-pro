import { useEffect, useRef, type ReactNode } from "react";

/** Native modal semantics keep focus and background isolation in the browser. */
export default function WorkspaceDialog({ open, title, closeLabel, busy, onClose, children, variant = "dialog", subtitle }: {
  open: boolean; title: string; closeLabel: string; busy: boolean;
  onClose: () => void; children: ReactNode; variant?: "dialog" | "inbound-editor"; subtitle?: string;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    if (!open || !element) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    element.showModal();
    element.querySelector<HTMLElement>("[data-initial-focus]")?.focus();
    return () => {
      element.close();
      document.body.style.overflow = overflow;
      if (opener?.isConnected && !opener.closest("[hidden], [inert]")) opener.focus();
      else document.querySelector<HTMLElement>("[data-workspace-focus]")?.focus();
    };
  }, [open]);
  return <dialog ref={dialog} className={"workspace-dialog" + (variant === "inbound-editor" ? " inbound-editor-dialog" : "")} aria-label={title}
    onKeyDown={event => {
      if (event.key !== "Tab") return;
      const items = Array.from(event.currentTarget.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
      )).filter(element => element.getClientRects().length > 0);
      const first = items[0], last = items.at(-1);
      if (!first || !last) { event.preventDefault(); return; }
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }}
    onCancel={event => { event.preventDefault(); if (!busy) onClose(); }}>
    <header><div><h2>{title}</h2>{subtitle && <p className="workspace-dialog-subtitle">{subtitle}</p>}</div><button type="button" disabled={busy} onClick={onClose} aria-label={closeLabel}>×</button></header>
    {open && children}
  </dialog>;
}
