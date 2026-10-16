package webui

import (
 "os"
 "strings"
 "testing"
)

func TestReleaseContainsCompiledUI(t *testing.T) {
 if os.Getenv("DARK_PRO_REQUIRE_EMBEDDED_UI")!="1" {t.Skip("release-builder-only compiled asset check")}
 raw,err:=bundled.ReadFile("dist/index.html")
 if err!=nil || !strings.Contains(string(raw),"id=\"root\"") || !strings.Contains(string(raw),"/assets/") {t.Fatal("release binary has no compiled application UI",err)}
 entries,err:=bundled.ReadDir("dist/assets");if err!=nil {t.Fatal(err)}
 hasJS,hasCSS:=false,false
 for _,entry:=range entries {hasJS=hasJS||strings.HasSuffix(entry.Name(),".js");hasCSS=hasCSS||strings.HasSuffix(entry.Name(),".css")}
 if !hasJS||!hasCSS {t.Fatal("compiled frontend assets missing")}
}
