package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

const (
	ownerToken = "test-owner-token-must-be-long-and-private-1234567890"
	nodeToken  = "1111111111111111111111111111111111111111111111111111111111111111"
	otherToken = "2222222222222222222222222222222222222222222222222222222222222222"
	installID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func request(t *testing.T, h http.Handler, method, route, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil { t.Fatal(err) }
	}
	req := httptest.NewRequest(method, route, bytes.NewReader(payload))
	if token != "" { req.Header.Set("Authorization", "Bearer "+token) }
	if body != nil { req.Header.Set("Content-Type", "application/json") }
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func newLab(t *testing.T) (*Server, *noderegistry.Registry, *time.Time, string) {
	t.Helper()
	now := time.Date(2026,10,12,8,0,0,0,time.UTC)
	path := filepath.Join(t.TempDir(),"private","nodes.json")
	reg,err := noderegistry.Open(path,func()time.Time{return now})
	if err!=nil {t.Fatal(err)}
	s,err:=NewServer(telemetry.NewStore(15*time.Second,func()time.Time{return now}),reg,ownerToken)
	if err!=nil {t.Fatal(err)}
	return s,reg,&now,path
}

func TestPairingIsOwnerIssuedAndAgentBound(t *testing.T) {
	s,_,_,_:=newLab(t)
	h:=s.Handler()
	input:=map[string]string{"node_id":"de-01","name":"Germany controlled"}
	if w:=request(t,h,"POST","/api/v1/admin/nodes/invites","",input);w.Code!=401 {
		t.Fatalf("anonymous owner action returned %d",w.Code)
	}
	if w:=request(t,h,"GET","/api/v1/admin/nodes","",nil);w.Code!=401 {
		t.Fatalf("owner list exposed without permission: %d",w.Code)
	}
	inviteResponse:=request(t,h,"POST","/api/v1/admin/nodes/invites",ownerToken,input)
	if inviteResponse.Code!=201 {t.Fatalf("invite %d %s",inviteResponse.Code,inviteResponse.Body.String())}
	var invite noderegistry.PairTicket
	if err:=json.Unmarshal(inviteResponse.Body.Bytes(),&invite);err!=nil{t.Fatal(err)}
	if invite.NodeID!="de-01"||len(invite.PairCode)!=64{t.Fatal("invalid ticket")}
	join:=map[string]string{
		"pair_code":invite.PairCode,"node_id":"de-01",
		"installation_id":installID,"agent_token":nodeToken,
	}
	if w:=request(t,h,"POST","/api/v1/agent/enroll","",join);w.Code!=200 {
		t.Fatalf("enroll %d %s",w.Code,w.Body.String())
	}
	// Lost response retry is safe with the exact same credential.
	if w:=request(t,h,"POST","/api/v1/agent/enroll","",join);w.Code!=200 {
		t.Fatalf("same claim cannot retry: %d",w.Code)
	}
	join["agent_token"]=otherToken
	if w:=request(t,h,"POST","/api/v1/agent/enroll","",join);w.Code!=409 {
		t.Fatalf("replayed code changed token: %d",w.Code)
	}
	fleetResponse:=request(t,h,"GET","/api/v1/fleet","",nil)
	var fleet telemetry.Fleet
	if err:=json.Unmarshal(fleetResponse.Body.Bytes(),&fleet);err!=nil{t.Fatal(err)}
	if fleet.Total!=1||fleet.Offline!=1||fleet.Online!=0||fleet.Nodes[0].NodeID!="de-01"||
		fleet.Nodes[0].LastSeen!=nil {
		t.Fatalf("newly paired Node should be offline until real heartbeat: %+v",fleet)
	}
	record:=telemetry.Sample{
		NodeID:"de-01",InstallationID:installID,Name:"FAKE RENAME",
		BootID:"cccccccccccccccccccccccccccccccc",Sequence:1,
		Interface:"eth0",CPUCores:4,CPUPercent:12.5,
		RAMTotal:1024,RAMUsed:200,RXBytes:100,TXBytes:100,
	}
	if w:=request(t,h,"POST","/api/v1/agent/heartbeat",ownerToken,record);w.Code!=401 {
		t.Fatalf("owner token impersonated Node: %d",w.Code)
	}
	if w:=request(t,h,"POST","/api/v1/agent/heartbeat",otherToken,record);w.Code!=401 {
		t.Fatalf("unpaired token impersonated Node: %d",w.Code)
	}
	if w:=request(t,h,"POST","/api/v1/agent/heartbeat",nodeToken,record);w.Code!=204 {
		t.Fatalf("valid sample %d %s",w.Code,w.Body.String())
	}
	if w:=request(t,h,"POST","/api/v1/agent/heartbeat",nodeToken,record);w.Code!=409 {
		t.Fatalf("replayed heartbeat renewed stale Node: %d",w.Code)
	}
	record.NodeID="nl-01"
	if w:=request(t,h,"POST","/api/v1/agent/heartbeat",nodeToken,record);w.Code!=401 {
		t.Fatalf("Agent spoofed another logical Node: %d",w.Code)
	}
	record.NodeID="de-01"
	record.InstallationID="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if w:=request(t,h,"POST","/api/v1/agent/heartbeat",nodeToken,record);w.Code!=401 {
		t.Fatalf("Agent impersonated other installation: %d",w.Code)
	}
	fleetResponse=request(t,h,"GET","/api/v1/fleet","",nil)
	if err:=json.Unmarshal(fleetResponse.Body.Bytes(),&fleet);err!=nil{t.Fatal(err)}
	if fleet.Total!=1||fleet.Online!=1||fleet.Nodes[0].Name!="Germany controlled"||
		fleet.Nodes[0].LastSeen==nil||fleet.Nodes[0].CPUCores!=4{
		t.Fatalf("valid real Node telemetry missing or Agent renamed Node: %+v",fleet)
	}
	if bytes.Contains(fleetResponse.Body.Bytes(),[]byte(nodeToken)) {
		t.Fatal("secret leaked in fleet response")
	}
}

func TestHubRestartPreservesRosterButNeverInventsOnlineStatus(t *testing.T) {
	s,_,now,path:=newLab(t)
	h:=s.Handler()
	inv:=request(t,h,"POST","/api/v1/admin/nodes/invites",ownerToken,map[string]string{"node_id":"us-01","name":"US lab"})
	var ticket noderegistry.PairTicket
	if err:=json.Unmarshal(inv.Body.Bytes(),&ticket);err!=nil{t.Fatal(err)}
	join:=map[string]string{"pair_code":ticket.PairCode,"node_id":"us-01","installation_id":installID,"agent_token":nodeToken}
	if w:=request(t,h,"POST","/api/v1/agent/enroll","",join);w.Code!=200{t.Fatal(w.Code)}
	record:=telemetry.Sample{NodeID:"us-01",InstallationID:installID,BootID:"cccccccccccccccccccccccccccccccc",
		Sequence:1,Interface:"ens3",CPUCores:8,CPUPercent:25,RAMTotal:2048,RAMUsed:1024}
	if w:=request(t,h,"POST","/api/v1/agent/heartbeat",nodeToken,record);w.Code!=204{t.Fatal(w.Code)}
	reg,err:=noderegistry.Open(path,func()time.Time{return *now})
	if err!=nil{t.Fatal(err)}
	restarted,err:=NewServer(telemetry.NewStore(15*time.Second,func()time.Time{return *now}),reg,ownerToken)
	if err!=nil{t.Fatal(err)}
	w:=request(t,restarted.Handler(),"GET","/api/v1/fleet","",nil)
	var snapshot telemetry.Fleet
	if err:=json.Unmarshal(w.Body.Bytes(),&snapshot);err!=nil{t.Fatal(err)}
	if snapshot.Total!=1||snapshot.Offline!=1||snapshot.Online!=0||
		snapshot.Nodes[0].Name!="US lab"||snapshot.Nodes[0].LastSeen!=nil {
		t.Fatalf("HUB restart lost Node or faked online: %+v",snapshot)
	}
}

func TestStrictJSONAndNewServerRejectsMissingOwnerSecret(t *testing.T) {
	s,reg,_,_:=newLab(t)
	if _,err:=NewServer(nil,reg,ownerToken);err==nil{t.Fatal("nil telemetry accepted")}
	if _,err:=NewServer(telemetry.NewStore(0,nil),reg,"short");err==nil{t.Fatal("unsafe lab key accepted")}
	h:=s.Handler()
	raw:=httptest.NewRequest("POST","/api/v1/admin/nodes/invites",bytes.NewBufferString(
		`{"node_id":"de-01","name":"Name","unapproved":true}`))
	raw.Header.Set("Authorization","Bearer "+ownerToken)
	w:=httptest.NewRecorder();h.ServeHTTP(w,raw)
	if w.Code!=400{t.Fatalf("unknown JSON fields accepted: %d",w.Code)}
	if w:=request(t,h,"GET","/api/v1/health","",nil);w.Code!=200{t.Fatal(w.Code)}
}

func TestPulseWithoutStatisticsStillReportsAuthenticOnline(t *testing.T) {
	s,_,now,_:=newLab(t)
	h:=s.Handler()
	inv:=request(t,h,"POST","/api/v1/admin/nodes/invites",ownerToken,
		map[string]string{"node_id":"nl-01","name":"Netherlands lab"})
	var ticket noderegistry.PairTicket
	if err:=json.Unmarshal(inv.Body.Bytes(),&ticket);err!=nil{t.Fatal(err)}
	join:=map[string]string{"pair_code":ticket.PairCode,"node_id":"nl-01",
		"installation_id":installID,"agent_token":nodeToken}
	if w:=request(t,h,"POST","/api/v1/agent/enroll","",join);w.Code!=200{t.Fatal(w.Code)}
	pulse:=telemetry.Pulse{NodeID:"nl-01",InstallationID:installID,
		BootID:"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",Sequence:1}
	if w:=request(t,h,"POST","/api/v1/agent/pulse",otherToken,pulse);w.Code!=401{
		t.Fatalf("unpaired token sent pulse: %d",w.Code)
	}
	if w:=request(t,h,"POST","/api/v1/agent/pulse",nodeToken,pulse);w.Code!=204{
		t.Fatalf("legitimate pulse rejected: %d %s",w.Code,w.Body.String())
	}
	w:=request(t,h,"GET","/api/v1/fleet","",nil)
	var fleet telemetry.Fleet
	if err:=json.Unmarshal(w.Body.Bytes(),&fleet);err!=nil{t.Fatal(err)}
	if fleet.Total!=1||fleet.Online!=1||fleet.Nodes[0].Name!="Netherlands lab"||
		fleet.Nodes[0].MetricsCurrent||fleet.Nodes[0].LastSeen==nil{
		t.Fatalf("a pulse-only Node must be Online with unknown metrics: %+v",fleet)
	}
	*now=now.Add(16*time.Second)
	if w:=request(t,h,"POST","/api/v1/agent/pulse",nodeToken,pulse);w.Code!=409{
		t.Fatalf("replayed pulse changed offline status: %d",w.Code)
	}
	w=request(t,h,"GET","/api/v1/fleet","",nil)
	if err:=json.Unmarshal(w.Body.Bytes(),&fleet);err!=nil{t.Fatal(err)}
	if fleet.Offline!=1||fleet.Nodes[0].Status!="offline"{
		t.Fatalf("offline transition failed: %+v",fleet)
	}
	pulse.Sequence=2
	if w:=request(t,h,"POST","/api/v1/agent/pulse",nodeToken,pulse);w.Code!=204{
		t.Fatalf("fresh Pulse did not recover Node: %d",w.Code)
	}
}
