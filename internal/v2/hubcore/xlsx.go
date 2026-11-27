package hubcore

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// A small .xlsx writer (no library): sheets of text and number cells with
// a bold header row, right-to-left when asked. Enough for reports that
// open in Excel, LibreOffice and Google Sheets.

// XSheet is one worksheet: the first row is the header.
type XSheet struct {
	Name string
	RTL  bool
	Rows [][]any // string, int, int64, float64 (anything else is printed)
}

func xmlEsc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// sheetName keeps a sheet name Excel accepts (31 characters, no []:*?/\).
func sheetName(n string, i int) string {
	n = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, n)
	if r := []rune(n); len(r) > 31 {
		n = string(r[:31])
	}
	if strings.TrimSpace(n) == "" {
		n = "Sheet" + strconv.Itoa(i+1)
	}
	return n
}

// WriteXLSX writes the workbook.
func WriteXLSX(w io.Writer, sheets []XSheet) error {
	z := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	var ct, wb, rels strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	wb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	used := map[string]bool{}
	for i, sh := range sheets {
		n := i + 1
		name := sheetName(sh.Name, i)
		for used[name] {
			name = sheetName(name+strconv.Itoa(n), i)
		}
		used[name] = true
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEsc(name), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		if err := put(fmt.Sprintf("xl/worksheets/sheet%d.xml", n), sheetXML(sh)); err != nil {
			return err
		}
	}
	ct.WriteString(`</Types>`)
	wb.WriteString(`</sheets></workbook>`)
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`, len(sheets)+1)
	for _, f := range []struct{ name, body string }{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", wb.String()},
		{"xl/_rels/workbook.xml.rels", rels.String()},
		{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.##"/></numFmts>` +
			`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
			`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
			`<cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
			`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
			`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs>` +
			`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`},
	} {
		if err := put(f.name, f.body); err != nil {
			return err
		}
	}
	return z.Close()
}

func sheetXML(sh XSheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"`)
	if sh.RTL {
		b.WriteString(` rightToLeft="1"`)
	}
	b.WriteString(`><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	// Column widths from the longest text in each column.
	widths := map[int]int{}
	for _, row := range sh.Rows {
		for c, v := range row {
			if n := len([]rune(fmt.Sprint(v))) + 2; n > widths[c] {
				widths[c] = min(n, 60)
			}
		}
	}
	if len(widths) > 0 {
		b.WriteString(`<cols>`)
		for c := 0; c < len(widths); c++ {
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, c+1, c+1, max(widths[c], 8))
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, row := range sh.Rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, v := range row {
			ref := colName(c) + strconv.Itoa(r+1)
			style := ""
			if r == 0 {
				style = ` s="1"`
			}
			switch x := v.(type) {
			case int:
				fmt.Fprintf(&b, `<c r="%s"%s><v>%d</v></c>`, ref, style, x)
			case int64:
				fmt.Fprintf(&b, `<c r="%s"%s><v>%d</v></c>`, ref, style, x)
			case float64:
				if style == "" {
					style = ` s="2"`
				}
				fmt.Fprintf(&b, `<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(x, 'f', -1, 64))
			case nil:
			default:
				fmt.Fprintf(&b, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, style, xmlEsc(fmt.Sprint(x)))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

// jalaliDate is a moment's Persian calendar date in Tehran ("1405/09/06").
func jalaliDate(u int64) string {
	t := time.Unix(u+tehranOffset, 0).UTC()
	jy, jm, jd := toJalali(t.Year(), int(t.Month()), t.Day())
	return fmt.Sprintf("%04d/%02d/%02d", jy, jm, jd)
}

func toJalali(gy, gm, gd int) (int, int, int) {
	gdm := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[gm-1]
	jy := -1595 + 33*(days/12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	if days < 186 {
		return jy, 1 + days/31, 1 + days%31
	}
	return jy, 7 + (days-186)/30, 1 + (days-186)%30
}
