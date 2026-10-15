// Command owner-setup performs ONE local bootstrap of a new PRO owner account.
// It never talks to production, accepts no password on argv and does not
// expose bootstrap over HTTP. Must run under the private new LAB service user.
package main

import (
 "context"
 "errors"
 "flag"
 "fmt"
 "log"
 "os"
 "path/filepath"
 "strings"
 "syscall"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func validateDatabasePath(path string)error{
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path||filepath.Ext(path)!=".sqlite"{
  return errors.New("business database must be an absolute new .sqlite path")
 }
 rel,err:=filepath.Rel("/var/lib/dark-xray-pro-lab",path)
 if err!=nil||rel=="."||rel==".."||strings.HasPrefix(rel,"../"){
  return errors.New("business database must reside under the new isolated LAB state directory")
 }
 return nil
}
func readPrivatePassword(path string)(string,error){
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path{return "",errors.New("password file must be absolute")}
 info,err:=os.Lstat(path)
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||
   info.Size()<12||info.Size()>259{return "",errors.New("owner password file must be private 0600/0400 and 12..256 bytes")}
 if stat,ok:=info.Sys().(*syscall.Stat_t); !ok||int(stat.Uid)!=os.Geteuid(){
  return "",errors.New("owner password file must be owned by the setup user")
 }
 raw,err:=os.ReadFile(path);if err!=nil{return "",err}
 defer func(){for i:=range raw{raw[i]=0}}()
 pwd:=strings.TrimSuffix(string(raw),"\n")
 pwd=strings.TrimSuffix(pwd,"\r")
 if strings.ContainsAny(pwd,"\n\r"){return "",errors.New("password file must contain exactly one password line")}
 if len(pwd)<12||len(pwd)>256{return "",errors.New("password length is invalid")}
 return pwd,nil
}
func main(){
 var dbPath,username,passwordFile string
 flag.StringVar(&dbPath,"business-db","","new isolated PRO business database")
 flag.StringVar(&username,"username","","first owner login name")
 flag.StringVar(&passwordFile,"password-file","","private file containing password (never pass password on argv)")
 flag.Parse()
 if os.Getenv("DARK_PRO_LAB_MODE")!="1"||os.Geteuid()==0{
  log.Fatal("setup requires explicit isolated LAB mode and an unprivileged service user")
 }
 if err:=validateDatabasePath(dbPath);err!=nil{log.Fatal(err)}
 password,err:=readPrivatePassword(passwordFile)
 if err!=nil{log.Fatal(err)}
 store,err:=business.Open(dbPath)
 if err!=nil{log.Fatal("cannot open private PRO business database: ",err)}
 defer store.Close()
 u,err:=store.BootstrapOwner(context.Background(),username,password)
 if err!=nil{log.Fatal("owner bootstrap refused; existing account cannot be overwritten: ",err)}
 fmt.Printf("Created isolated PRO owner login %s (private database only; no Telegram or Xray changes).\n",u.Username)
}
