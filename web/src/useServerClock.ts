import { useRef } from "react";

// Local renders must not restart the age of an unchanged server observation.
// A later sample can advance the clock, but never move its estimate backward.
export function useServerClock(serverNow:number):()=>number {
 const clock=useRef<{sample:number;server:number;mono:number;wall:number}|null>(null);
 const mono=performance.now(),wall=Date.now(),prior=clock.current;
 if(Number.isFinite(serverNow)&&serverNow>0&&serverNow<=9_223_372_036_854&&(!prior||serverNow>prior.sample)){
  const previous=prior?prior.server+Math.max(0,mono-prior.mono,wall-prior.wall):serverNow;
  clock.current={sample:serverNow,server:Math.max(serverNow,previous),mono,wall};
 }
 return ()=>{const c=clock.current;return c?c.server+Math.max(0,performance.now()-c.mono,Date.now()-c.wall):Infinity;};
}
