package hubcore

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReportWorkbookIsAValidXLSX(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali & Co", Enabled: true})
	_, _ = s.db.Exec(`INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,0,'<n>')`, now.Add(-time.Hour).Unix(), "ali", "ali", "charge", -(5 << 30))
	for _, rep := range []string{"", "ali"} {
		sheets, err := s.ReportWorkbook(ctx, rep, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(sheets[0].Rows) != 1+7+1 {
			t.Fatalf("daily rows: %d", len(sheets[0].Rows))
		}
		var buf bytes.Buffer
		if err := WriteXLSX(&buf, sheets); err != nil {
			t.Fatal(err)
		}
		z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, f := range z.File {
			names[f.Name] = true
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			d := xml.NewDecoder(bytes.NewReader(b))
			for {
				if _, err := d.Token(); err != nil {
					if err != io.EOF {
						t.Fatalf("%s: bad XML: %v", f.Name, err)
					}
					break
				}
			}
			if rep == "ali" && f.Name == "xl/worksheets/sheet2.xml" && !strings.Contains(string(b), "&lt;n&gt;") {
				t.Fatal("the ledger note must be escaped")
			}
		}
		for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
			if !names[n] {
				t.Fatalf("missing %s", n)
			}
		}
	}
	if colName(0) != "A" || colName(25) != "Z" || colName(26) != "AA" || colName(27) != "AB" {
		t.Fatal("column names")
	}
	if jalaliDate(time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC).Unix()) != "1405/09/06" {
		t.Fatalf("jalali: %s", jalaliDate(now.Unix()))
	}
}
