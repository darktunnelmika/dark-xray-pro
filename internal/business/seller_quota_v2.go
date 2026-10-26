package business

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "time"
)

// SellerQuotaV2 is an escrow against the EXISTING representatives credit row.
// There is no cv2_wallets or independently mintable second source of money.
// All balances and receipts are int64; JSON exposes large amounts as strings.
type SellerQuotaV2 struct {
 ID string `json:"id"`
 RepresentativeID string `json:"representative_id"`
 Name string `json:"name"`
 TelegramUserID int64 `json:"telegram_user_id,string"`
 Enabled bool `json:"enabled"`
 ReservedBytes int64 `json:"reserved_bytes,string"`
 ReservedUnits int64 `json:"reserved_units,string"`
}

type CreateSellerV2 struct {
 EventID string `json:"event_id"`
 RepresentativeID string `json:"representative_id"`
 SellerID string `json:"seller_id"`
 Name string `json:"name"`
 TelegramUserID int64 `json:"telegram_user_id"`
}

type SellerQuotaTransferV2 struct {
 EventID string `json:"event_id"`
 RepresentativeID string `json:"representative_id"`
 SellerID string `json:"seller_id"`
 Action string `json:"action"` // allocate | reclaim
 Bytes int64 `json:"bytes"`
 Units int64 `json:"units"`
 Reason string `json:"reason"`
}

type SellerQuotaReceiptV2 struct {
 EventID string `json:"event_id"`
 RepresentativeID string `json:"representative_id"`
 SellerID string `json:"seller_id"`
 Action string `json:"action"`
 RepFreeBytes int64 `json:"representative_free_bytes,string"`
 RepFreeUnits int64 `json:"representative_free_units,string"`
 SellerReservedBytes int64 `json:"seller_reserved_bytes,string"`
 SellerReservedUnits int64 `json:"seller_reserved_units,string"`
 Replayed bool `json:"replayed"`
}

// EnableSellerQuotaV2 is an EXPLICIT, additive schema gate. Do not call from
// Store.Open or automatically on deployed HUB. The owned rollout requires
// its own encrypted backup, schema acceptance and checked startup flag.
func (s *Store) EnableSellerQuotaV2(ctx context.Context) error {
 tx, err := s.db.BeginTx(ctx,nil)
 if err != nil { return err }
 defer tx.Rollback()
 ddl:=[]string{
 `CREATE TABLE IF NOT EXISTS seller_quota_v2_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
 `CREATE TABLE IF NOT EXISTS representative_sellers_v2(
   id TEXT NOT NULL, representative_id TEXT NOT NULL REFERENCES representatives(id),
   name TEXT NOT NULL, telegram_user_id INTEGER NOT NULL CHECK(telegram_user_id>0),
   enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
   reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK(reserved_bytes>=0 AND reserved_bytes<=1000000000000000000),
   reserved_units INTEGER NOT NULL DEFAULT 0 CHECK(reserved_units>=0 AND reserved_units<=1000000000),
   created_unix INTEGER NOT NULL,
   PRIMARY KEY(representative_id,id), UNIQUE(representative_id,telegram_user_id))`,
 `CREATE INDEX IF NOT EXISTS seller_v2_parent_idx ON representative_sellers_v2(representative_id,id)`,
 `CREATE TABLE IF NOT EXISTS seller_quota_events_v2(
   event_id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL,
   representative_id TEXT NOT NULL REFERENCES representatives(id),
   seller_id TEXT NOT NULL,
   action TEXT NOT NULL CHECK(action IN ('create','allocate','reclaim')),
   receipt_json TEXT NOT NULL, reason TEXT NOT NULL, created_unix INTEGER NOT NULL,
   FOREIGN KEY(representative_id,seller_id)
     REFERENCES representative_sellers_v2(representative_id,id))`,
 `CREATE TRIGGER IF NOT EXISTS seller_v2_events_no_update BEFORE UPDATE ON seller_quota_events_v2
   BEGIN SELECT RAISE(ABORT,'seller quota audit immutable'); END`,
 `CREATE TRIGGER IF NOT EXISTS seller_v2_events_no_delete BEFORE DELETE ON seller_quota_events_v2
   BEGIN SELECT RAISE(ABORT,'seller quota audit immutable'); END`,
 `INSERT OR IGNORE INTO seller_quota_v2_meta(id,version) VALUES(1,1)`,
 }
 for _,statement:=range ddl {
  if _,err=tx.ExecContext(ctx,statement);err!=nil{return err}
 }
 var v int
 if err=tx.QueryRowContext(ctx,"SELECT version FROM seller_quota_v2_meta WHERE id=1").Scan(&v);err!=nil||v!=1{return ErrUnavailable}
 return tx.Commit()
}

func sellerV2Scan(row interface{Scan(...any)error})(SellerQuotaV2,error){
 var v SellerQuotaV2
 var enabled int
 err:=row.Scan(&v.ID,&v.RepresentativeID,&v.Name,&v.TelegramUserID,&enabled,&v.ReservedBytes,&v.ReservedUnits)
 v.Enabled=enabled==1
 return v,err
}
const sellerV2Columns="id,representative_id,name,telegram_user_id,enabled,reserved_bytes,reserved_units"

func (s *Store) SellerV2(ctx context.Context,representativeID,sellerID string)(SellerQuotaV2,error){
 if !idRE.MatchString(representativeID)||!idRE.MatchString(sellerID){return SellerQuotaV2{},ErrInvalid}
 v,e:=sellerV2Scan(s.db.QueryRowContext(ctx,"SELECT "+sellerV2Columns+
   " FROM representative_sellers_v2 WHERE representative_id=? AND id=?",representativeID,sellerID))
 if errors.Is(e,sql.ErrNoRows){return SellerQuotaV2{},ErrNotFound}
 if e!=nil{return SellerQuotaV2{},ErrUnavailable}
 return v,nil
}

func (s *Store) ListSellersV2(ctx context.Context,representativeID string,limit,offset int)([]SellerQuotaV2,error){
 if !idRE.MatchString(representativeID)||limit<1||limit>100||offset<0||offset>1_000_000{return nil,ErrInvalid}
 rows,e:=s.db.QueryContext(ctx,"SELECT "+sellerV2Columns+
  " FROM representative_sellers_v2 WHERE representative_id=? ORDER BY id LIMIT ? OFFSET ?",
  representativeID,limit,offset)
 if e!=nil{return nil,ErrUnavailable}
 defer rows.Close()
 list:=make([]SellerQuotaV2,0)
 for rows.Next(){v,e:=sellerV2Scan(rows);if e!=nil{return nil,ErrUnavailable};list=append(list,v)}
 if rows.Err()!=nil{return nil,ErrUnavailable}
 return list,nil
}

func sellerReceiptV2(ctx context.Context,tx *sql.Tx,eventID,hash string)(SellerQuotaReceiptV2,bool,error){
 var previous,raw string
 e:=tx.QueryRowContext(ctx,"SELECT fingerprint,receipt_json FROM seller_quota_events_v2 WHERE event_id=?",eventID).Scan(&previous,&raw)
 if errors.Is(e,sql.ErrNoRows){return SellerQuotaReceiptV2{},false,nil}
 if e!=nil{return SellerQuotaReceiptV2{},false,ErrUnavailable}
 if previous!=hash{return SellerQuotaReceiptV2{},false,ErrConflict}
 var result SellerQuotaReceiptV2
 if json.Unmarshal([]byte(raw),&result)!=nil || result.EventID!=eventID{return SellerQuotaReceiptV2{},false,ErrUnavailable}
 result.Replayed=true
 return result,true,nil
}
func sellerWriteReceiptV2(ctx context.Context,tx *sql.Tx,receipt SellerQuotaReceiptV2,hash,reason string)error{
 raw,err:=json.Marshal(receipt)
 if err!=nil{return ErrUnavailable}
 _,err=tx.ExecContext(ctx,`INSERT INTO seller_quota_events_v2
  (event_id,fingerprint,representative_id,seller_id,action,receipt_json,reason,created_unix)
  VALUES(?,?,?,?,?,?,?,?)`,receipt.EventID,hash,receipt.RepresentativeID,receipt.SellerID,
  receipt.Action,string(raw),reason,time.Now().UTC().Unix())
 return err
}
func sellerCurrentV2(ctx context.Context,tx *sql.Tx,repID,sellerID string)(Representative,SellerQuotaV2,error){
 rep,e:=readRep(ctx,tx,repID)
 if errors.Is(e,sql.ErrNoRows){return Representative{},SellerQuotaV2{},ErrNotFound}
 if e!=nil{return Representative{},SellerQuotaV2{},ErrUnavailable}
 s,e:=sellerV2Scan(tx.QueryRowContext(ctx,"SELECT "+sellerV2Columns+
  " FROM representative_sellers_v2 WHERE id=? AND representative_id=?",sellerID,repID))
 if errors.Is(e,sql.ErrNoRows){return Representative{},SellerQuotaV2{},ErrNotFound}
 if e!=nil{return Representative{},SellerQuotaV2{},ErrUnavailable}
 return rep,s,nil
}

func (s *Store) CreateSellerV2(ctx context.Context,input CreateSellerV2)(SellerQuotaV2,error){
 if !eventRE.MatchString(input.EventID)||!idRE.MatchString(input.RepresentativeID)||
  input.RepresentativeID=="owner"||!idRE.MatchString(input.SellerID)||
  !validName(input.Name,128)||input.TelegramUserID<=0 {return SellerQuotaV2{},ErrInvalid}
 hash:=fingerprint("seller-v2-create",input)
 tx,e:=s.db.BeginTx(ctx,nil);if e!=nil{return SellerQuotaV2{},ErrUnavailable}
 defer tx.Rollback()
 receipt,replayed,e:=sellerReceiptV2(ctx,tx,input.EventID,hash)
 if e!=nil{return SellerQuotaV2{},e}
 if replayed{
  _,seller,e:=sellerCurrentV2(ctx,tx,input.RepresentativeID,input.SellerID)
  if e!=nil{return SellerQuotaV2{},e}
  if receipt.SellerID!=seller.ID{return SellerQuotaV2{},ErrConflict}
  return seller,nil
 }
 rep,e:=readRep(ctx,tx,input.RepresentativeID)
 if errors.Is(e,sql.ErrNoRows){return SellerQuotaV2{},ErrNotFound}
 if e!=nil{return SellerQuotaV2{},ErrUnavailable}
 if rep.Kind!="representative"||!rep.Enabled{return SellerQuotaV2{},ErrConflict}
 _,e=tx.ExecContext(ctx,`INSERT INTO representative_sellers_v2
 (id,representative_id,name,telegram_user_id,enabled,reserved_bytes,reserved_units,created_unix)
 VALUES(?,?,?,?,1,0,0,?)`,input.SellerID,input.RepresentativeID,input.Name,input.TelegramUserID,time.Now().UTC().Unix())
 if e!=nil{return SellerQuotaV2{},ErrConflict}
 result:=SellerQuotaV2{ID:input.SellerID,RepresentativeID:input.RepresentativeID,
  Name:input.Name,TelegramUserID:input.TelegramUserID,Enabled:true}
 receipt=SellerQuotaReceiptV2{EventID:input.EventID,RepresentativeID:rep.ID,SellerID:input.SellerID,
  Action:"create",RepFreeBytes:rep.VolumeCreditBytes,RepFreeUnits:rep.UnlimitedCreditUnits}
 if e=sellerWriteReceiptV2(ctx,tx,receipt,hash,"seller_created");e!=nil{return SellerQuotaV2{},ErrUnavailable}
 if e=tx.Commit();e!=nil{return SellerQuotaV2{},ErrUnavailable}
 return result,nil
}

// TransferSellerQuotaV2 is a single atomic transfer between the existing
// representative available credit and seller reserve. Seller reserve cannot
// be spent by any existing Client API. Future sales MUST atomically debit it
// while creating and delivering a real client; never credit the rep again.
func (s *Store) TransferSellerQuotaV2(ctx context.Context,input SellerQuotaTransferV2)(SellerQuotaReceiptV2,error){
 if !eventRE.MatchString(input.EventID)||!idRE.MatchString(input.RepresentativeID)||
  input.RepresentativeID=="owner"||!idRE.MatchString(input.SellerID)||
  (input.Action!="allocate"&&input.Action!="reclaim")||!validName(input.Reason,200)||
  input.Bytes<0||input.Units<0||(input.Bytes==0&&input.Units==0)||
  input.Bytes>MaxVolumeCredit||input.Units>MaxUnlimitedCredit {return SellerQuotaReceiptV2{},ErrInvalid}
 hash:=fingerprint("seller-v2-transfer",input)
 tx,e:=s.db.BeginTx(ctx,nil);if e!=nil{return SellerQuotaReceiptV2{},ErrUnavailable}
 defer tx.Rollback()
 receipt,replayed,e:=sellerReceiptV2(ctx,tx,input.EventID,hash)
 if e!=nil{return SellerQuotaReceiptV2{},e}
 if replayed{return receipt,nil}
 rep,seller,e:=sellerCurrentV2(ctx,tx,input.RepresentativeID,input.SellerID)
 if e!=nil{return SellerQuotaReceiptV2{},e}
 if rep.Kind!="representative"||input.Action=="allocate"&&(!rep.Enabled||!seller.Enabled){
  return SellerQuotaReceiptV2{},ErrConflict
 }
 deltaBytes,deltaUnits:=input.Bytes,input.Units
 if input.Action=="allocate"{deltaBytes=-deltaBytes;deltaUnits=-deltaUnits}
 repVolume,ok:=safeAdd(rep.VolumeCreditBytes,deltaBytes,MaxVolumeCredit)
 if !ok{return SellerQuotaReceiptV2{},ErrInsufficient}
 repUnits,ok:=safeAdd(rep.UnlimitedCreditUnits,deltaUnits,MaxUnlimitedCredit)
 if !ok{return SellerQuotaReceiptV2{},ErrInsufficient}
 sellerVolume,ok:=safeAdd(seller.ReservedBytes,-deltaBytes,MaxVolumeCredit)
 if !ok{return SellerQuotaReceiptV2{},ErrInsufficient}
 sellerUnits,ok:=safeAdd(seller.ReservedUnits,-deltaUnits,MaxUnlimitedCredit)
 if !ok{return SellerQuotaReceiptV2{},ErrInsufficient}
 _,e=tx.ExecContext(ctx,"UPDATE representatives SET volume_credit_bytes=?, unlimited_credit_units=? WHERE id=?",
   repVolume,repUnits,input.RepresentativeID)
 if e!=nil{return SellerQuotaReceiptV2{},ErrUnavailable}
 _,e=tx.ExecContext(ctx,`UPDATE representative_sellers_v2 SET reserved_bytes=?,reserved_units=?
   WHERE id=? AND representative_id=?`,sellerVolume,sellerUnits,input.SellerID,input.RepresentativeID)
 if e!=nil{return SellerQuotaReceiptV2{},ErrUnavailable}
 receipt=SellerQuotaReceiptV2{EventID:input.EventID,RepresentativeID:input.RepresentativeID,
  SellerID:input.SellerID,Action:input.Action,RepFreeBytes:repVolume,RepFreeUnits:repUnits,
  SellerReservedBytes:sellerVolume,SellerReservedUnits:sellerUnits}
 if e=sellerWriteReceiptV2(ctx,tx,receipt,hash,input.Reason);e!=nil{return SellerQuotaReceiptV2{},ErrUnavailable}
 if e=tx.Commit();e!=nil{return SellerQuotaReceiptV2{},ErrUnavailable}
 return receipt,nil
}
