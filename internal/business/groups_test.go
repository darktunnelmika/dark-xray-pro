package business

import (
 "context"
 "errors"
 "fmt"
 "path/filepath"
 "testing"
)

func groupTestClient(t *testing.T,s *Store,rep,id string)Client{
 t.Helper()
 c,err:=s.CreateClient(context.Background(),NewClient{
  EventID:"create-group-client-"+id,ClientID:id,RepresentativeID:rep,
  Name:"Test "+id,VolumeBytes:100,DurationDays:30,IPCount:0,
 })
 if err!=nil{t.Fatalf("test client failed (%s): %v",id,err)}
 return c
}
func groupTestRep(t *testing.T,s *Store,id string){
 t.Helper()
 newRep(t,s,id)
 grant(t,s,id,"grant-group-credit-"+id,10000,100)
}
func TestClientGroupsBulkAssignMoveAndDeletePreserveAllClientsAndAccounting(t *testing.T){
 ctx:=context.Background();s,path:=opened(t)
 groupTestRep(t,s,"rep-A")
 a:=groupTestClient(t,s,"rep-A","native-A1")
 b:=groupTestClient(t,s,"rep-A","native-A2")
 g1,err:=s.CreateGroup(ctx,NewGroup{ID:"group-A1",RepresentativeID:"rep-A",Name:"VIP Clients",Color:"#123ABC"})
 if err!=nil||g1.MemberCount!=0{t.Fatalf("new group invalid: %+v %v",g1,err)}
 again,err:=s.CreateGroup(ctx,NewGroup{ID:"group-A1",RepresentativeID:"rep-A",Name:"VIP Clients",Color:"#123ABC"})
 if err!=nil||again.ID!=g1.ID{t.Fatalf("same create failed to be idempotent: %+v %v",again,err)}
 g2,err:=s.CreateGroup(ctx,NewGroup{ID:"group-A2",RepresentativeID:"rep-A",Name:"New Group",Color:""})
 if err!=nil{t.Fatal(err)}
 g1,err=s.AssignGroupMembers(ctx,g1.ID,"rep-A",[]string{a.ID,b.ID})
 if err!=nil||g1.MemberCount!=2{t.Fatalf("bulk assign failed: %+v %v",g1,err)}
 g1,err=s.AssignGroupMembers(ctx,g1.ID,"rep-A",[]string{a.ID,b.ID})
 if err!=nil||g1.MemberCount!=2{t.Fatal("repeated assignment duplicated members",err)}
 g2,err=s.AssignGroupMembers(ctx,g2.ID,"rep-A",[]string{b.ID})
 if err!=nil||g2.MemberCount!=1{t.Fatal(err)}
 g1,err=s.Group(ctx,"group-A1")
 if err!=nil||g1.MemberCount!=1{t.Fatalf("moving client did not update old group: %+v %v",g1,err)}
 members,err:=s.ListGroupMembers(ctx,g2.ID,"rep-A",20,0)
 if err!=nil||len(members)!=1||members[0]!=b.ID{t.Fatalf("moved membership not persisted: %+v %v",members,err)}
 if err=s.RemoveGroupMember(ctx,g2.ID,"rep-A",b.ID);err!=nil{t.Fatal(err)}
 if err=s.RemoveGroupMember(ctx,g2.ID,"rep-A",b.ID);!errors.Is(err,ErrNotFound){t.Fatalf("repeated removal pretended success: %v",err)}
 if g1,err=s.UpdateGroup(ctx,g1.ID,"rep-A","VIP Renamed","#Ff00aa");err!=nil||g1.Name!="VIP Renamed"{
  t.Fatalf("rename failed: %+v %v",g1,err)
 }
 detached,err:=s.DeleteGroup(ctx,g1.ID,"rep-A")
 if err!=nil||detached!=1{t.Fatalf("delete did not detach one member: %d %v",detached,err)}
 for _,id:=range []string{a.ID,b.ID}{
  got,e:=s.Client(ctx,id)
  if e!=nil||got.ID!=id||got.DeploymentState!="pending_node_apply"{
   t.Fatalf("group delete lost real client: %+v %v",got,e)
  }
 }
 events,err:=s.ListEvents(ctx,"rep-A",20,0)
 if err!=nil||len(events)!=3{t.Fatalf("group delete touched historical ledger: %+v %v",events,err)}
 balance,err:=s.Representative(ctx,"rep-A")
 if err!=nil||balance.VolumeCreditBytes!=9800{t.Fatalf("group delete changed representative credit: %+v %v",balance,err)}
 if err=s.Close();err!=nil{t.Fatal(err)}
 recovered,err:=Open(path)
 if err!=nil{t.Fatal(err)}
 defer recovered.Close()
 g2,err=recovered.Group(ctx,g2.ID)
 if err!=nil||g2.MemberCount!=0{t.Fatalf("restart lost group state: %+v %v",g2,err)}
 if _,err=recovered.Group(ctx,"group-A1");!errors.Is(err,ErrNotFound){t.Fatal("deleted group reappeared")}
}
func TestBulkMembershipRejectsCrossTenantMissingAndOversizedBatchAtomically(t *testing.T){
 ctx:=context.Background();s,_:=opened(t)
 groupTestRep(t,s,"rep-1")
 groupTestRep(t,s,"rep-2")
 c1:=groupTestClient(t,s,"rep-1","native-good")
 c2:=groupTestClient(t,s,"rep-2","native-other")
 g,err:=s.CreateGroup(ctx,NewGroup{ID:"group-good",RepresentativeID:"rep-1",Name:"Safe",Color:""})
 if err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{members []string;want error}{
  {[]string{c1.ID,c2.ID},ErrConflict},
  {[]string{c1.ID,"missing-id"},ErrNotFound},
  {[]string{c1.ID,c1.ID},ErrInvalid},
 }{
  if _,err:=s.AssignGroupMembers(ctx,g.ID,"rep-1",tc.members);!errors.Is(err,tc.want){
   t.Fatalf("mixed batch did not reject atomically: %v (want %v)",err,tc.want)
  }
  state,err:=s.Group(ctx,g.ID)
  if err!=nil||state.MemberCount!=0{t.Fatalf("partial assignment leaked: %+v %v",state,err)}
 }
 oversized:=make([]string,501)
 for i:=range oversized{oversized[i]=fmt.Sprintf("synthetic-%03d",i)}
 if _,err:=s.AssignGroupMembers(ctx,g.ID,"rep-1",oversized);!errors.Is(err,ErrInvalid){t.Fatal("unbounded bulk accepted")}
 if _,err:=s.AssignGroupMembers(ctx,g.ID,"rep-2",[]string{c2.ID});!errors.Is(err,ErrConflict){
  t.Fatalf("cross-tenant group action succeeded: %v",err)
 }
 if _,err:=s.UpdateGroup(ctx,g.ID,"rep-2","Hijacked","");!errors.Is(err,ErrConflict){
  t.Fatal("cross-tenant group rename succeeded")
 }
 if _,err:=s.DeleteGroup(ctx,g.ID,"rep-2");!errors.Is(err,ErrConflict){
  t.Fatal("cross-tenant delete succeeded")
 }
 if err:=s.RemoveGroupMember(ctx,g.ID,"rep-2",c2.ID);!errors.Is(err,ErrConflict){
  t.Fatal("cross-tenant remove succeeded")
 }
}
func TestGroupNamesColorsAndIdentityConflicts(t *testing.T){
 ctx:=context.Background();s,_:=opened(t)
 groupTestRep(t,s,"rep-color")
 if _,err:=s.CreateGroup(ctx,NewGroup{ID:"bad",RepresentativeID:"rep-color",Name:"Bad",Color:"red;url(alert)"});!errors.Is(err,ErrInvalid){
  t.Fatalf("CSS injected color accepted: %v",err)
 }
 if _,err:=s.CreateGroup(ctx,NewGroup{ID:"bad",RepresentativeID:"rep-color",Name:"\n",Color:""});!errors.Is(err,ErrInvalid){
  t.Fatal("empty/control name accepted")
 }
 if _,err:=s.CreateGroup(ctx,NewGroup{ID:"one",RepresentativeID:"rep-color",Name:"VIP",Color:"#abc123"});err!=nil{t.Fatal(err)}
 if _,err:=s.CreateGroup(ctx,NewGroup{ID:"two",RepresentativeID:"rep-color",Name:"VIP",Color:""});!errors.Is(err,ErrConflict){
  t.Fatal("duplicate group label accepted")
 }
 if _,err:=s.CreateGroup(ctx,NewGroup{ID:"one",RepresentativeID:"rep-color",Name:"VIP2",Color:""});!errors.Is(err,ErrConflict){
  t.Fatal("existing group id changed via replay")
 }
 if rows,err:=s.ListGroups(ctx,"rep-color",50,0);err!=nil||len(rows)!=1{t.Fatalf("group not listed: %v %v",rows,err)}
}
func TestBusinessV2ToV3GroupsMigrationPreservesSessionCreditsAndClients(t *testing.T){
 ctx:=context.Background()
 path:=filepath.Join(t.TempDir(),"private","upgraded.sqlite")
 s,err:=Open(path);if err!=nil{t.Fatal(err)}
 groupTestRep(t,s,"rep-existing")
 client:=groupTestClient(t,s,"rep-existing","already-live-record")
 if _,err=s.BootstrapOwner(ctx,"ownerpro","initial-owner-strong-password");err!=nil{t.Fatal(err)}
 session,err:=s.Login(ctx,"ownerpro","initial-owner-strong-password")
 if err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  "DROP TRIGGER native_dirty_profile_update", // remove post-v6 lifecycle predicate before dropping its columns
  "DROP TRIGGER native_plan_block_enable",
  "DROP TABLE client_operations",
  "ALTER TABLE clients DROP COLUMN archived",
  "DROP TABLE client_group_members",
  "DROP TABLE client_groups",
  "DROP TABLE native_inbound_operations","ALTER TABLE native_inbounds DROP COLUMN revision","ALTER TABLE native_inbounds DROP COLUMN archived","ALTER TABLE native_inbounds DROP COLUMN enabled","PRAGMA user_version=2",
 }{
  if _,err=s.db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 if err=s.Close();err!=nil{t.Fatal(err)}
 upgraded,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer upgraded.Close()
 if _,err=upgraded.VerifySession(ctx,session.Token);err!=nil{t.Fatalf("schema upgrade logged owner out: %v",err)}
 c,err:=upgraded.Client(ctx,client.ID)
 if err!=nil||c.UUID!=client.UUID{t.Fatalf("client lost across v2→v3: %+v %v",c,err)}
 r,err:=upgraded.Representative(ctx,"rep-existing")
 if err!=nil||r.VolumeCreditBytes!=9900{t.Fatalf("credit lost across v2→v3: %+v %v",r,err)}
 events,err:=upgraded.ListEvents(ctx,"rep-existing",50,0)
 if err!=nil||len(events)!=2{t.Fatalf("history lost across migration: %d %v",len(events),err)}
 if _,err:=upgraded.CreateGroup(ctx,NewGroup{ID:"new-group",RepresentativeID:"rep-existing",Name:"Imported later"});err!=nil{
  t.Fatalf("group schema not usable after upgrade: %v",err)
 }
}
func TestGroupDeletionDoesNotDropRawBillingRecords(t *testing.T){
 s,_:=opened(t);ctx:=context.Background()
 groupTestRep(t,s,"rep-proof")
 c:=groupTestClient(t,s,"rep-proof","client-proof")
 g,err:=s.CreateGroup(ctx,NewGroup{ID:"proof-group",RepresentativeID:"rep-proof",Name:"Historic"})
 if err!=nil{t.Fatal(err)}
 if _,err=s.AssignGroupMembers(ctx,g.ID,"rep-proof",[]string{c.ID});err!=nil{t.Fatal(err)}
 if _,err=s.DeleteGroup(ctx,g.ID,"rep-proof");err!=nil{t.Fatal(err)}
 var clientCount,eventCount int
 if err=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM clients WHERE id=?",c.ID).Scan(&clientCount);err!=nil{t.Fatal(err)}
 if err=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM events WHERE representative_id=?",c.RepresentativeID).Scan(&eventCount);err!=nil{t.Fatal(err)}
 if clientCount!=1||eventCount!=2{t.Fatalf("financial records deleted: clients=%d ledger=%d",clientCount,eventCount)}
}
