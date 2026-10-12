package main

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/routecheck"
)

// pollPreflight pulls only an owner-approved task for THIS paired installation,
// performs bounded TCP connectivity checks, and acknowledges the evidence.
// It NEVER writes an Xray config, replaces an old node, or publishes a sub.
func pollPreflight(ctx context.Context, client *http.Client, endpoint,nodeID,installationID,token string)error{
 query:=url.Values{}
 query.Set("node_id",nodeID)
 query.Set("installation_id",installationID)
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,
  endpoint+"/api/v1/agent/replacement-task?"+query.Encode(),nil)
 if err!=nil{return err}
 req.Header.Set("Authorization","Bearer "+token)
 response,err:=client.Do(req)
 if err!=nil{return err}
 var operation replacement.Operation
 func() {
  defer response.Body.Close()
  if response.StatusCode==http.StatusOK{
   err=json.NewDecoder(io.LimitReader(response.Body,24*1024)).Decode(&operation)
  }
 }()
 if err!=nil{return err}
 switch response.StatusCode{
 case http.StatusNoContent:return nil
 case http.StatusOK:
 default:return fmt.Errorf("replacement task retrieval returned HTTP %d",response.StatusCode)
 }
 if operation.Phase!=replacement.Pending||operation.CanActivate||
  operation.ID==""||operation.NewNodeID!=nodeID||
  operation.InstallationID!=installationID||
  !operation.NeedsXrayValidation||!operation.NeedsDataPlaneProbe||
  operation.Preview.ManifestSHA256==""{
  return errors.New("replacement task is untrusted or not eligible")
 }
 checkCtx,cancel:=context.WithTimeout(ctx,10*time.Second)
 result,err:=routecheck.Run(checkCtx,operation,nil)
 cancel()
 if err!=nil{return err}
 data,err:=json.Marshal(result.Evidence)
 if err!=nil{return err}
 report,err:=http.NewRequestWithContext(ctx,http.MethodPost,
  endpoint+"/api/v1/agent/replacement-report",bytes.NewReader(data))
 if err!=nil{return err}
 report.Header.Set("Authorization","Bearer "+token)
 report.Header.Set("Content-Type","application/json")
 receipt,err:=client.Do(report)
 if err!=nil{return err}
 defer receipt.Body.Close()
 if receipt.StatusCode!=http.StatusOK{
  _,_=io.Copy(io.Discard,io.LimitReader(receipt.Body,512))
  return fmt.Errorf("replacement evidence rejected: HTTP %d",receipt.StatusCode)
 }
 var ack replacement.Operation
 if err:=json.NewDecoder(io.LimitReader(receipt.Body,24*1024)).Decode(&ack);err!=nil{return err}
 if ack.ID!=operation.ID||ack.NewNodeID!=nodeID||
  ack.Preview.ManifestSHA256!=operation.Preview.ManifestSHA256||
  ack.CanActivate||ack.Phase==replacement.Pending{
  return errors.New("replacement probe acknowledgement mismatch")
 }
 return nil
}
