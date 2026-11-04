import { QRCodeCanvas } from "qrcode.react";
import type { Language } from "../session";

// Render locally: subscription credentials must never leave the browser for
// an image service. The parent owns the same reveal lifetime as the URL.
export default function SubscriptionQRCode({ url, language }: { url: string; language: Language }) {
  const label = language === "fa" ? "کد QR اشتراک" : "Subscription QR code";
  return <figure className="subscription-qr">
    <QRCodeCanvas value={url} size={256} level="M" marginSize={4}
      bgColor="#ffffff" fgColor="#000000" role="img" aria-label={label}
      style={{ width: 256, maxWidth: "100%", height: "auto" }} />
    <figcaption>{language === "fa" ? "برای افزودن اشتراک، این کد را در برنامهٔ کلاینت اسکن کنید." : "Scan in your client app to add this subscription."}</figcaption>
  </figure>;
}
