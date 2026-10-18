import { useEffect, useState } from "react";

// Store only bounded page offsets per authenticated account and tab.
// Never persist customer records, credentials, CSRF tokens or financial data.
export function useAccountPageOffset(account: string, view: string, pageSize = 50) {
  const key = "dark-pro-offset:" + account + ":" + view;
  const [offset, setOffset] = useState(() => {
    try {
      const n = Number(sessionStorage.getItem(key));
      return Number.isSafeInteger(n) && n >= 0 && n <= 1_000_000 && Number.isSafeInteger(pageSize) && pageSize >= 1 && pageSize <= 200 && n % pageSize === 0 ? n : 0;
    } catch { return 0; }
  });
  useEffect(() => {
    try {
      if (offset === 0) sessionStorage.removeItem(key);
      else sessionStorage.setItem(key, String(offset));
    } catch { /* restricted storage */ }
  }, [key, offset]);
  return [offset, setOffset] as const;
}
