package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerUIAlwaysLoopbackAndAgentPortIsIndependent(t *testing.T) {
	for _, valid := range []string{"127.0.0.1:2087","[::1]:2087"} {
		if !loopbackAddr(valid) {t.Errorf("safe owner UI address refused: %s",valid)}
	}
	for _, invalid := range []string{":2087","0.0.0.0:2087","[::]:2087","198.51.100.20:2087","bad"} {
		if loopbackAddr(invalid) {t.Errorf("exposed owner UI address allowed: %s",invalid)}
	}
	for _, valid := range []string{":24443","0.0.0.0:8443","[::1]:24443"} {
		if err:=separateAgentPort(valid,"127.0.0.1:2087");err!=nil {
			t.Errorf("independent agent port refused: %s: %v",valid,err)
		}
	}
	for _, invalid := range []string{":2087","0.0.0.0:2087",":443",":65536","invalid"} {
		if err:=separateAgentPort(invalid,"127.0.0.1:2087");err==nil {
			t.Errorf("agent listener port validation bypassed: %s",invalid)
		}
	}
}

func TestRemoteTLSKeyMustBePrivateAndRegular(t *testing.T) {
	dir:=t.TempDir()
	key:=filepath.Join(dir,"agent.key")
	if err:=os.WriteFile(key,[]byte("placeholder"),0644);err!=nil{t.Fatal(err)}
	if err:=privateTLSKey(key);err==nil {t.Fatal("world-readable private key allowed")}
	if err:=os.Chmod(key,0600);err!=nil{t.Fatal(err)}
	if err:=privateTLSKey(key);err!=nil{t.Fatal(err)}
	link:=filepath.Join(dir,"symlink.key")
	if err:=os.Symlink(key,link);err!=nil{t.Fatal(err)}
	if err:=privateTLSKey(link);err==nil{t.Fatal("symlinked private key allowed")}
	if err:=privateTLSKey(dir);err==nil{t.Fatal("directory treated as TLS key")}
	if err:=privateTLSKey(filepath.Join(dir,"missing.key"));err==nil{t.Fatal("missing private key allowed")}
}
