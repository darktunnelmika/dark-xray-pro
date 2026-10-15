package main

import (
 "os"
 "path/filepath"
 "testing"
)

func TestOwnerSetupReadsPrivatePasswordOnly(t *testing.T){
 path:=filepath.Join(t.TempDir(),"owner-password")
 if err:=os.WriteFile(path,[]byte("unique-password-just-for-lab\n"),0600);err!=nil{t.Fatal(err)}
 value,err:=readPrivatePassword(path)
 if err!=nil||value!="unique-password-just-for-lab"{t.Fatalf("valid private password not read: %v",err)}
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err=readPrivatePassword(path);err==nil{t.Fatal("world-readable password file accepted")}
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 link:=filepath.Join(t.TempDir(),"password-symlink")
 if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err=readPrivatePassword(link);err==nil{t.Fatal("symlinked owner password accepted")}
 if err:=os.WriteFile(path,[]byte("good-password-2026\nextra-line"),0600);err!=nil{t.Fatal(err)}
 if _,err=readPrivatePassword(path);err==nil{t.Fatal("multiline file accepted")}
}
func TestOwnerSetupNeverAcceptsLegacyDatabasePath(t *testing.T){
 for _,path:=range []string{
  "", "/var/lib/dark-xray/data/dark.sqlite3",
  "/var/lib/dark-xray-pro-lab", "/var/lib/dark-xray-pro-lab/../dark-xray/data.sqlite",
  "/tmp/sneaky.sqlite", "../private.sqlite", "/var/lib/dark-xray-pro-lab/not-db.db",
 }{
  if err:=validateDatabasePath(path);err==nil{t.Errorf("unsafe path accepted: %q",path)}
 }
 if err:=validateDatabasePath("/var/lib/dark-xray-pro-lab/private/business.sqlite");err!=nil{
  t.Fatal("new isolated database refused:",err)
 }
}
