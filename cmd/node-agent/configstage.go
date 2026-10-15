package main

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"

 "github.com/darktunnelmika/dark-xray-pro/internal/configstage"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func proofCode(err error)string {
 switch {
 case errors.Is(err,xrayconfig.ErrCoreUnavailable):
  return "binary_unavailable"
 case errors.Is(err,xrayconfig.ErrCoreMismatch):
  return "binary_digest_mismatch"
 case errors.Is(err,xrayconfig.ErrCandidateInvalid):
  return "candidate_invalid"
 case errors.Is(err,xrayconfig.ErrCoreRejected):
  return "xray_rejected"
 default:
  return "validation_error"
 }
}

// pollConfigStage reads Reality PRIVATE bytes only from an already pinned,
// CA-verified HTTPS Agent listener and never writes them into the running
// Xray service, Node desired-state, subscriptions or unprotected log files.
func pollConfigStage(ctx context.Context,client *http.Client,endpoint,nodeID,
 installation,token,xrayBinary,trustedXraySHA string)error {
 if !strings.HasPrefix(endpoint,"https://") || client==nil{
  return errors.New("private Xray staging requires verified HTTPS Agent transport")
 }
 query:=url.Values{}
 query.Set("node_id",nodeID)
 query.Set("installation_id",installation)
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,
  endpoint+"/api/v1/agent/config-stage?"+query.Encode(),nil)
 if err!=nil{return err}
 req.Header.Set("Authorization","Bearer "+token)
 res,err:=client.Do(req)
 if err!=nil{return err}
 var delivery configstage.Delivery
 func(){
  defer res.Body.Close()
  if res.StatusCode==http.StatusOK{
   decoder:=json.NewDecoder(io.LimitReader(res.Body,2*xrayconfig.MaxCandidateBytes+8192))
   decoder.DisallowUnknownFields()
   err=decoder.Decode(&delivery)
  }
 }()
 if err!=nil{return err}
 if res.StatusCode==http.StatusNoContent{return nil}
 if res.StatusCode!=http.StatusOK{
  return fmt.Errorf("LAB Agent stage retrieval failed: HTTP %d",res.StatusCode)
 }
 m:=delivery.Metadata
 if m.Status!=configstage.Pending||m.CanActivate||
  !m.NeedsDataPlaneProof||!m.NeedsSubscriptionCommit||
  m.NodeID!=nodeID||m.InstallationID!=installation||
  m.TrustedXraySHA256!=trustedXraySHA||
  len(m.ManifestSHA256)!=64||len(m.ConfigSHA256)!=64||
  len(m.OperationID)!=32 {
  return errors.New("stage metadata does not match paired Node and trusted binary identity")
 }
 raw,err:=base64.StdEncoding.Strict().DecodeString(delivery.ConfigBase64)
 if err!=nil||len(raw)==0||len(raw)>xrayconfig.MaxCandidateBytes{
  return errors.New("invalid secret candidate envelope")
 }
 digest:=sha256.Sum256(raw)
 if hex.EncodeToString(digest[:])!=m.ConfigSHA256{
  return errors.New("private candidate SHA mismatch")
 }
 candidate:=xrayconfig.Candidate{
  Config:raw,SHA256:m.ConfigSHA256,ManifestSHA256:m.ManifestSHA256,
  CanActivate:false,RequiresXrayTest:true,
  RequiresDataPlaneProof:true,RequiresSubscriptionCommit:true,
 }
 validation,err:=xrayconfig.ValidateOffline(ctx,candidate,xrayBinary,trustedXraySHA)
 proof:=configstage.Proof{
  OperationID:m.OperationID,NodeID:nodeID,InstallationID:installation,
  ConfigSHA256:m.ConfigSHA256,ManifestSHA256:m.ManifestSHA256,
  TrustedXraySHA256:trustedXraySHA,
 }
 if err==nil{
  if !validation.XraySyntaxValidated||validation.CanActivate||
   !validation.NeedsDataPlaneProof||!validation.NeedsSubscriptionCommit||
   validation.ConfigSHA256!=m.ConfigSHA256||
   validation.ManifestSHA256!=m.ManifestSHA256||
   validation.PinnedXraySHA256!=trustedXraySHA{
   return errors.New("offline validator returned inconsistent evidence")
  }
  proof.SyntaxValid=true
 }else{
  proof.ErrorCode=proofCode(err)
 }
 // The Reality private data is not included in this response.
 payload,err:=json.Marshal(proof)
 if err!=nil{return err}
 post,err:=http.NewRequestWithContext(ctx,http.MethodPost,
  endpoint+"/api/v1/agent/config-stage-report",bytes.NewReader(payload))
 if err!=nil{return err}
 post.Header.Set("Authorization","Bearer "+token)
 post.Header.Set("Content-Type","application/json")
 response,err:=client.Do(post)
 if err!=nil{return err}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{
  _,_=io.Copy(io.Discard,io.LimitReader(response.Body,512))
  return fmt.Errorf("Xray validation receipt rejected: HTTP %d",response.StatusCode)
 }
 var ack configstage.Metadata
 if err:=json.NewDecoder(io.LimitReader(response.Body,2048)).Decode(&ack);err!=nil{return err}
 if ack.OperationID!=m.OperationID||ack.InstallationID!=installation||
  ack.ConfigSHA256!=m.ConfigSHA256||ack.TrustedXraySHA256!=trustedXraySHA||
  ack.CanActivate||!ack.NeedsDataPlaneProof||!ack.NeedsSubscriptionCommit||
  (proof.SyntaxValid&&ack.Status!=configstage.Verified)||
  (!proof.SyntaxValid&&ack.Status!=configstage.Rejected){
  return errors.New("HUB returned an untrusted Xray validation acknowledgment")
 }
 return nil
}
