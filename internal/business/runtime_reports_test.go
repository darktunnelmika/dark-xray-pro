package business

import (
 "bytes"
 "context"
 "errors"
 "testing"
 "time"
 "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)
func setupRuntimeReport(t *testing.T)(*Store,NodeConfig,managedxray.RuntimeReport){
 t.Helper();s,_:=opened(t);ctx:=context.Background();key:=bytes.Repeat([]byte{7},32)
 if _,e:=s.CreateInbound(ctx,key,nativeInput());e!=nil{t.Fatal(e)}
 if e:=s.PutInboundTarget(ctx,InboundTarget{InboundID:10,NodeID:"new-node",InstallationID:inboundInstall,ListenIP:"127.0.0.1",DirectPort:19443});e!=nil{t.Fatal(e)}
 cfg,e:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if e!=nil{t.Fatal(e)}
 now:=time.Now();lease,e:=managedxray.SignLease(bytes.Repeat([]byte{9},64),managedxray.Lease{NodeID:cfg.NodeID,InstallationID:cfg.InstallationID,Sequence:cfg.Sequence,ConfigSHA:cfg.ConfigSHA,ManifestSHA:cfg.ManifestSHA,Issued:now.UnixNano(),Expires:now.Add(30*time.Second).UnixNano()});if e!=nil{t.Fatal(e)}
 if e=s.RecordNativeDelivery(ctx,cfg,lease);e!=nil{t.Fatal(e)}
 report:=managedxray.RuntimeReport{Lease:lease,Observed:managedxray.Snapshot{Phase:"ready",Highwater:cfg.Sequence,ActiveSequence:cfg.Sequence,ActiveSHA:cfg.ConfigSHA,SavedSequence:cfg.Sequence,SavedSHA:cfg.ConfigSHA,ProcessReady:true,LeaseExpires:lease.Expires}}
 return s,cfg,report
}
func TestRuntimeReportFreshnessAndReplayCannotInventApply(t *testing.T){
 s,cfg,r:=setupRuntimeReport(t);ctx:=context.Background()
 state,e:=s.NativeRuntimeStatus(ctx,cfg.NodeID,cfg.InstallationID)
 if e!=nil||state.State!="awaiting_report"||state.ProcessReady{t.Fatalf("invented report: %+v %v",state,e)}
 if e=s.AcceptNativeReport(ctx,r);e!=nil{t.Fatal(e)}
 state,e=s.NativeRuntimeStatus(ctx,cfg.NodeID,cfg.InstallationID)
 if e!=nil||state.State!="applied"||!state.ProcessReady||state.DataPlaneVerified||state.SubscriptionPublished{t.Fatalf("bad report status %+v %v",state,e)}
 before:=state.ReportedAt;if e=s.AcceptNativeReport(ctx,r);e!=nil{t.Fatal(e)}
 state,_=s.NativeRuntimeStatus(ctx,cfg.NodeID,cfg.InstallationID);if state.ReportedAt!=before{t.Fatal("duplicate report refreshed freshness")}
 changed:=r;changed.Observed.Phase="applying";if e=s.AcceptNativeReport(ctx,changed);!errors.Is(e,ErrConflict){t.Fatal("same delivery replaced observation",e)}
 if _,e=s.db.Exec("UPDATE native_runtime_reports SET received=?",time.Now().Add(-13*time.Second).UnixNano());e!=nil{t.Fatal(e)}
 if e=s.AcceptNativeReport(ctx,r);e!=nil{t.Fatal(e)};state,_=s.NativeRuntimeStatus(ctx,cfg.NodeID,cfg.InstallationID)
 if state.State!="stale"||state.ProcessReady{t.Fatalf("replay revived stale status: %+v",state)}
}
func TestRuntimeReportsNeverMaskConfigurationChanges(t *testing.T){
 s,cfg,r:=setupRuntimeReport(t);ctx:=context.Background();if e:=s.AcceptNativeReport(ctx,r);e!=nil{t.Fatal(e)}
 c,e:=s.CreateClient(ctx,NewClient{EventID:"new-report-client-0001",ClientID:"report-user",RepresentativeID:"owner",Name:"New client",DurationDays:30,VolumeBytes:10});if e!=nil{t.Fatal(e)}
 if e=s.BindInboundClients(ctx,10,[]string{c.ID},true);e!=nil{t.Fatal(e)}
 status,e:=s.NativeRuntimeStatus(ctx,cfg.NodeID,cfg.InstallationID)
 if e!=nil||status.State!="configuration_changed"||status.ProcessReady{t.Fatalf("change showed old applied report: %+v %v",status,e)}
 nextLease:=r.Lease;nextLease.Issued++;if e=s.RecordNativeDelivery(ctx,cfg,nextLease);!errors.Is(e,ErrConflict){t.Fatalf("old candidate issued after change: %v",e)}
 if _,e=s.Client(ctx,c.ID);e!=nil{t.Fatal("report handling deleted client",e)}
}
func TestRuntimeReportsRequireActualDeliveryAndBoundEvidence(t *testing.T){
 s,_,r:=setupRuntimeReport(t);ctx:=context.Background();tests:=[]managedxray.RuntimeReport{}
 x:=r;x.Lease.Issued++;tests=append(tests,x)
 x=r;x.Observed.ActiveSHA="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";tests=append(tests,x)
 x=r;x.Observed.DataPlaneVerified=true;tests=append(tests,x)
 x=r;x.Observed.SubscriptionPublished=true;tests=append(tests,x)
 x=r;x.Observed.Phase="fake_success";tests=append(tests,x)
 x=r;x.Lease.Expires=time.Now().Add(-time.Second).UnixNano();tests=append(tests,x)
 x=r;x.Lease.InstallationID="22222222222222222222222222222222";tests=append(tests,x)
 for i,q:=range tests{if e:=s.AcceptNativeReport(ctx,q);e==nil{t.Fatalf("unsafe report %d accepted",i)}}
 if e:=s.AcceptNativeReport(ctx,r);e!=nil{t.Fatal(e)}
 x=r;x.Lease.Issued+=int64(time.Millisecond);x.Lease.Expires+=int64(time.Millisecond)
 cfg,e:=s.BuildNodeConfig(ctx,bytes.Repeat([]byte{7},32),r.Lease.NodeID,r.Lease.InstallationID);if e!=nil{t.Fatal(e)}
 if e=s.RecordNativeDelivery(ctx,cfg,x.Lease);e!=nil{t.Fatal(e)}
 if e=s.AcceptNativeReport(ctx,r);!errors.Is(e,ErrConflict){t.Fatal("superseded receipt accepted",e)}
 x.Observed.ProcessReady=false;x.Observed.ActiveSHA="";x.Observed.ActiveSequence=0;x.Observed.Phase="lease_expired_stopped"
 if e=s.AcceptNativeReport(ctx,x);e!=nil{t.Fatal(e)};status,_:=s.NativeRuntimeStatus(ctx,cfg.NodeID,cfg.InstallationID)
 if status.State!="stopped"||status.ProcessReady{t.Fatalf("stop report invented ready %+v",status)}
}
func TestRuntimeReportV5MigrationPreservesBusinessIdentity(t *testing.T){
 s,path:=opened(t);ctx:=context.Background();newRep(t,s,"report-rep");grant(t,s,"report-rep","grant-report-upgrade1",1000,50)
 c,e:=s.CreateClient(ctx,NewClient{EventID:"new-before-report-v6",ClientID:"before-report",RepresentativeID:"report-rep",Name:"Preserved",DurationDays:30,VolumeBytes:100});if e!=nil{t.Fatal(e)}
 rows,e:=s.db.Query("SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'native_dirty_%'");if e!=nil{t.Fatal(e)}
 names:=[]string{};for rows.Next(){var name string;if e=rows.Scan(&name);e!=nil{rows.Close();t.Fatal(e)};names=append(names,name)}
 e=rows.Err();rows.Close();if e!=nil{t.Fatal(e)}
 for _,name:=range names{if _,e=s.db.Exec("DROP TRIGGER "+name);e!=nil{t.Fatal(e)}}
 for _,q:=range []string{"DROP TABLE native_runtime_reports","DROP TABLE native_runtime_delivery","DROP TABLE native_config_generation","DROP TABLE native_inbound_operations","ALTER TABLE native_inbounds DROP COLUMN revision","ALTER TABLE native_inbounds DROP COLUMN archived","ALTER TABLE native_inbounds DROP COLUMN enabled","PRAGMA user_version=5"}{if _,e=s.db.Exec(q);e!=nil{t.Fatal(e)}}
 if e=s.Close();e!=nil{t.Fatal(e)};up,e:=Open(path);if e!=nil{t.Fatal(e)};defer up.Close()
 kept,e:=up.Client(ctx,c.ID);if e!=nil||kept.UUID!=c.UUID{t.Fatal("v6 lost identity",e)}
 rep,e:=up.Representative(ctx,"report-rep");if e!=nil||rep.VolumeCreditBytes!=900||rep.UnlimitedCreditUnits!=50{t.Fatal("v6 changed credit",e)}
}
func TestNativeCreateRetryPreservesKeyAndBindings(t *testing.T){
 s,_,_:=setupRuntimeReport(t);ctx:=context.Background();key:=bytes.Repeat([]byte{7},32)
 before,e:=s.ListInbounds(ctx,key);if e!=nil{t.Fatal(e)}
 again,e:=s.CreateInbound(ctx,key,nativeInput());if e!=nil||again.PublicKey!=before[0].PublicKey{t.Fatal("create retry changed key",e)}
 altered:=nativeInput();altered.Name="Unapproved replacement"
 if _,e=s.CreateInbound(ctx,key,altered);!errors.Is(e,ErrConflict){t.Fatal("different inbound payload replaced original",e)}
}
