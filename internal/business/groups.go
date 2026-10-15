package business

import (
 "context"
 "database/sql"
 "errors"
 "regexp"
 "strings"
 "time"
)

const maxGroupMembersPerRequest=500
var groupColorRE=regexp.MustCompile("^(|#[a-fA-F0-9]{6})$")

type NewGroup struct{
 ID string `json:"id"`
 RepresentativeID string `json:"representative_id"`
 Name string `json:"name"`
 Color string `json:"color"`
}
type Group struct{
 ID string `json:"id"`
 RepresentativeID string `json:"representative_id"`
 Name string `json:"name"`
 Color string `json:"color"`
 MemberCount int64 `json:"member_count"`
 CreatedUnix int64 `json:"created_unix"`
 UpdatedUnix int64 `json:"updated_unix"`
}
type AssignGroupMembers struct{
 ClientIDs []string `json:"client_ids"`
}

// Migration from v2 never drops, modifies or scans customer identity, ledger,
// representative credit, sessions or Telegram values. Group membership has
// exactly one mapping per client and is never a billable usage source.
func migrateGroups(db *sql.DB)error{
 tx,err:=db.Begin();if err!=nil{return err}
 defer tx.Rollback()
 statements:=[]string{
  "CREATE TABLE IF NOT EXISTS client_groups (id TEXT PRIMARY KEY, representative_id TEXT NOT NULL REFERENCES representatives(id), name TEXT NOT NULL, color TEXT NOT NULL DEFAULT '', created_unix INTEGER NOT NULL, updated_unix INTEGER NOT NULL, UNIQUE(representative_id,name))",
  "CREATE INDEX IF NOT EXISTS client_groups_rep_name ON client_groups(representative_id,name)",
  "CREATE TABLE IF NOT EXISTS client_group_members (client_id TEXT PRIMARY KEY REFERENCES clients(id) ON DELETE RESTRICT, group_id TEXT NOT NULL REFERENCES client_groups(id) ON DELETE CASCADE, assigned_unix INTEGER NOT NULL)",
  "CREATE INDEX IF NOT EXISTS client_group_members_group ON client_group_members(group_id,client_id)",
  "PRAGMA user_version=3",
 }
 for _,stmt:=range statements{if _,err=tx.Exec(stmt);err!=nil{return err}}
 return tx.Commit()
}
func groupScan(row interface{Scan(...any)error})(Group,error){
 var g Group
 err:=row.Scan(&g.ID,&g.RepresentativeID,&g.Name,&g.Color,&g.MemberCount,&g.CreatedUnix,&g.UpdatedUnix)
 return g,err
}
const groupCols="g.id,g.representative_id,g.name,g.color,(SELECT COUNT(*) FROM client_group_members m WHERE m.group_id=g.id),g.created_unix,g.updated_unix"
func readGroup(ctx context.Context,tx *sql.Tx,id string)(Group,error){
 return groupScan(tx.QueryRowContext(ctx,"SELECT "+groupCols+" FROM client_groups g WHERE g.id=?",id))
}
func validGroup(n NewGroup)bool{
 return idRE.MatchString(n.ID)&&idRE.MatchString(n.RepresentativeID)&&
  validName(n.Name,80)&&groupColorRE.MatchString(n.Color)
}
func(s *Store)CreateGroup(ctx context.Context,n NewGroup)(Group,error){
 if !validGroup(n){return Group{},ErrInvalid}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Group{},err}
 defer tx.Rollback()
 rep,err:=readRep(ctx,tx,n.RepresentativeID)
 if errors.Is(err,sql.ErrNoRows){return Group{},ErrNotFound}
 if err!=nil{return Group{},err}
 if !rep.Enabled{return Group{},ErrConflict}
 existing,err:=readGroup(ctx,tx,n.ID)
 if err==nil{
  if existing.RepresentativeID==n.RepresentativeID&&existing.Name==n.Name&&existing.Color==n.Color{
   return existing,nil // safe retry does not recreate or detach members
  }
  return Group{},ErrConflict
 }
 if !errors.Is(err,sql.ErrNoRows){return Group{},err}
 now:=time.Now().UTC().Unix()
 _,err=tx.ExecContext(ctx,"INSERT INTO client_groups(id,representative_id,name,color,created_unix,updated_unix) VALUES(?,?,?,?,?,?)",n.ID,n.RepresentativeID,n.Name,n.Color,now,now)
 if err!=nil{return Group{},ErrConflict}
 if err=tx.Commit();err!=nil{return Group{},err}
 return Group{ID:n.ID,RepresentativeID:n.RepresentativeID,Name:n.Name,Color:n.Color,CreatedUnix:now,UpdatedUnix:now,MemberCount:0},nil
}
func(s *Store)ListGroups(ctx context.Context,repID string,limit,offset int)([]Group,error){
 if repID!=""&&!idRE.MatchString(repID)||limit<1||limit>200||offset<0||offset>1_000_000{
  return nil,ErrInvalid
 }
 query:="SELECT "+groupCols+" FROM client_groups g"
 var args []any
 if repID!=""{query+=" WHERE g.representative_id=?";args=append(args,repID)}
 query+=" ORDER BY g.name,g.id LIMIT ? OFFSET ?"
 args=append(args,limit,offset)
 rows,err:=s.db.QueryContext(ctx,query,args...)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=make([]Group,0)
 for rows.Next(){g,e:=groupScan(rows);if e!=nil{return nil,e};out=append(out,g)}
 return out,rows.Err()
}
func(s *Store)Group(ctx context.Context,id string)(Group,error){
 if !idRE.MatchString(id){return Group{},ErrInvalid}
 g,err:=groupScan(s.db.QueryRowContext(ctx,"SELECT "+groupCols+" FROM client_groups g WHERE g.id=?",id))
 if errors.Is(err,sql.ErrNoRows){return Group{},ErrNotFound}
 return g,err
}
func(s *Store)UpdateGroup(ctx context.Context,id,repID,name,color string)(Group,error){
 if !idRE.MatchString(id)||!idRE.MatchString(repID)||!validName(name,80)||!groupColorRE.MatchString(color){
  return Group{},ErrInvalid
 }
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Group{},err}
 defer tx.Rollback()
 g,err:=readGroup(ctx,tx,id)
 if errors.Is(err,sql.ErrNoRows){return Group{},ErrNotFound}
 if err!=nil{return Group{},err}
 if g.RepresentativeID!=repID{return Group{},ErrConflict}
 _,err=tx.ExecContext(ctx,"UPDATE client_groups SET name=?,color=?,updated_unix=? WHERE id=?",name,color,time.Now().UTC().Unix(),id)
 if err!=nil{return Group{},ErrConflict}
 g,err=readGroup(ctx,tx,id);if err!=nil{return Group{},err}
 if err=tx.Commit();err!=nil{return Group{},err}
 return g,nil
}
// Deleting a group detaches mappings ONLY. Every client/credit/audit
// survives; a group is not the source of truth for billable usage.
func(s *Store)DeleteGroup(ctx context.Context,id,repID string)(int64,error){
 if !idRE.MatchString(id)||!idRE.MatchString(repID){return 0,ErrInvalid}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return 0,err}
 defer tx.Rollback()
 g,err:=readGroup(ctx,tx,id)
 if errors.Is(err,sql.ErrNoRows){return 0,ErrNotFound}
 if err!=nil{return 0,err}
 if g.RepresentativeID!=repID{return 0,ErrConflict}
 if _,err=tx.ExecContext(ctx,"DELETE FROM client_groups WHERE id=?",id);err!=nil{return 0,err}
 if err=tx.Commit();err!=nil{return 0,err}
 return g.MemberCount,nil
}
func validClientIDBatch(ids []string)bool{
 if len(ids)==0||len(ids)>maxGroupMembersPerRequest{return false}
 seen:=make(map[string]bool,len(ids))
 for _,id:=range ids{
  if !idRE.MatchString(id)||seen[id]{return false}
  seen[id]=true
 }
 return true
}
func(s *Store)AssignGroupMembers(ctx context.Context,groupID,repID string,clientIDs []string)(Group,error){
 if !idRE.MatchString(groupID)||!idRE.MatchString(repID)||!validClientIDBatch(clientIDs){
  return Group{},ErrInvalid
 }
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Group{},err}
 defer tx.Rollback()
 g,err:=readGroup(ctx,tx,groupID)
 if errors.Is(err,sql.ErrNoRows){return Group{},ErrNotFound}
 if err!=nil{return Group{},err}
 if g.RepresentativeID!=repID{return Group{},ErrConflict}
 // Authenticate ALL clients before changing any mapping, then update in one
 // transaction. Mixed-tenant/missing clients cause full rollback, no partial.
 args:=make([]any,0,len(clientIDs))
 placeholders:=make([]string,0,len(clientIDs))
 for _,id:=range clientIDs{args=append(args,id);placeholders=append(placeholders,"?")}
 rows,err:=tx.QueryContext(ctx,
  "SELECT id,representative_id FROM clients WHERE id IN ("+strings.Join(placeholders,",")+")",args...)
 if err!=nil{return Group{},err}
 found:=map[string]bool{}
 for rows.Next(){
  var id,owner string
  if err=rows.Scan(&id,&owner);err!=nil{rows.Close();return Group{},err}
  if owner!=repID{rows.Close();return Group{},ErrConflict}
  found[id]=true
 }
 if err=rows.Err();err!=nil{rows.Close();return Group{},err}
 if err=rows.Close();err!=nil{return Group{},err}
 if len(found)!=len(clientIDs){return Group{},ErrNotFound}
 now:=time.Now().UTC().Unix()
 for _,clientID:=range clientIDs{
  _,err=tx.ExecContext(ctx,`INSERT INTO client_group_members(client_id,group_id,assigned_unix)
 VALUES(?,?,?) ON CONFLICT(client_id) DO UPDATE SET
 group_id=excluded.group_id,assigned_unix=excluded.assigned_unix
 WHERE client_group_members.group_id!=excluded.group_id`,clientID,groupID,now)
  if err!=nil{return Group{},err}
 }
 g,err=readGroup(ctx,tx,groupID);if err!=nil{return Group{},err}
 if err=tx.Commit();err!=nil{return Group{},err}
 return g,nil
}
func(s *Store)ListGroupMembers(ctx context.Context,id,repID string,limit,offset int)([]string,error){
 if !idRE.MatchString(id)||!idRE.MatchString(repID)||limit<1||limit>200||offset<0||offset>1_000_000{
  return nil,ErrInvalid
 }
 g,err:=s.Group(ctx,id)
 if err!=nil{return nil,err}
 if g.RepresentativeID!=repID{return nil,ErrConflict}
 rows,err:=s.db.QueryContext(ctx,"SELECT client_id FROM client_group_members WHERE group_id=? ORDER BY client_id LIMIT ? OFFSET ?",id,limit,offset)
 if err!=nil{return nil,err}
 defer rows.Close()
 ids:=make([]string,0)
 for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{return nil,err};ids=append(ids,id)}
 return ids,rows.Err()
}
func(s *Store)RemoveGroupMember(ctx context.Context,id,repID,clientID string)error{
 if !idRE.MatchString(id)||!idRE.MatchString(repID)||!idRE.MatchString(clientID){return ErrInvalid}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err}
 defer tx.Rollback()
 g,err:=readGroup(ctx,tx,id)
 if errors.Is(err,sql.ErrNoRows){return ErrNotFound}
 if err!=nil{return err}
 if g.RepresentativeID!=repID{return ErrConflict}
 result,err:=tx.ExecContext(ctx,"DELETE FROM client_group_members WHERE group_id=? AND client_id=?",id,clientID)
 if err!=nil{return err}
 count,err:=result.RowsAffected()
 if err!=nil{return err}
 if count!=1{return ErrNotFound}
 return tx.Commit()
}
