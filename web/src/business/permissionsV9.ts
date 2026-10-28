import type { AppUser, BusinessPermission } from "../session";
// UI presentation only. The server remains authoritative for every route.
export function businessGranted(user:AppUser,right:BusinessPermission):boolean{
  if(right==="manage_sellers")return user.role==="owner"||user.permissions?.manage_sellers===true;
  return user.role==="owner"||user.permissions?.[right]!==false;
}
