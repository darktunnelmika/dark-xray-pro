import {useEffect,useRef,type ReactNode} from "react";
export default function ClientDialog({open,onClose,title,closeLabel,onRefresh,refreshLabel,children}:{open:boolean;onClose:()=>void;title:string;closeLabel:string;onRefresh?:()=>void;refreshLabel?:string;children:ReactNode}){
 const ref=useRef<HTMLDialogElement>(null);
 useEffect(()=>{const d=ref.current;if(!d)return;if(open&&!d.open)d.showModal();else if(!open&&d.open)d.close();},[open]);
 useEffect(()=>{if(!open)return;const old=document.body.style.overflow;document.body.style.overflow="hidden";return()=>{document.body.style.overflow=old;};},[open]);
 return <dialog ref={ref} className="client-dialog" aria-label={title} onCancel={e=>{e.preventDefault();onClose();}}>
  <header className="client-dialog-heading"><strong>{title}</strong><div className="client-dialog-actions">{onRefresh&&<button type="button" onClick={onRefresh}>{refreshLabel}</button>}<button type="button" onClick={onClose}>{closeLabel}</button></div></header>
  <div className="client-dialog-content">{children}</div>
 </dialog>;
}
