//go:build linux

package managedxray

import("bytes";"context";"crypto/hmac";"encoding/json";"errors";"io";"net/http")
// Bounded metadata only; a report is not independent data-plane proof.
type RuntimeReport struct{Lease Lease `json:"lease"`;Observed Snapshot `json:"observed"`}
func ValidReportSignature(r RuntimeReport,token string)bool{return len(token)>=32&&len(r.Lease.Signature)==64&&hmac.Equal([]byte(r.Lease.Signature),[]byte(r.Lease.signature([]byte(token))))}
func reportObservation(ctx context.Context,client *http.Client,endpoint,token string,report RuntimeReport)error{
 raw,e:=json.Marshal(report);if e!=nil{return e}
 req,e:=http.NewRequestWithContext(ctx,http.MethodPost,endpoint+"/api/v1/agent/native-runtime-report",bytes.NewReader(raw));if e!=nil{return e}
 req.Header.Set("Authorization","Bearer "+token);req.Header.Set("Content-Type","application/json")
 res,e:=client.Do(req);if e!=nil{return errors.New("native runtime report unavailable")};defer res.Body.Close();_,_=io.Copy(io.Discard,io.LimitReader(res.Body,512))
 if res.StatusCode!=http.StatusNoContent{return errors.New("native runtime report refused")};return nil
}
