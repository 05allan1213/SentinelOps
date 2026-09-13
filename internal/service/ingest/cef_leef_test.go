package ingest

import "testing"

func TestParseCEFExtractsNameSeverityAndAddresses(t *testing.T) {
	alert, err := ParseCEF("CEF:0|Int2|Probe|1.0|100|CEF probe event|7|src=10.0.0.9 dst=10.0.0.1 spt=4444", "src-name")
	if err != nil {
		t.Fatalf("parse CEF: %v", err)
	}
	if alert.Title != "CEF probe event" {
		t.Fatalf("title=%q", alert.Title)
	}
	if alert.Severity != "high" {
		t.Fatalf("severity=%q want high", alert.Severity)
	}
	if alert.Source != "src-name" {
		t.Fatalf("source=%q", alert.Source)
	}
	if alert.ExtraFields["src_ip"] != "10.0.0.9" || alert.ExtraFields["dst_ip"] != "10.0.0.1" {
		t.Fatalf("extra=%v", alert.ExtraFields)
	}
	if alert.IngestSource != "cef" {
		t.Fatalf("ingest source=%q", alert.IngestSource)
	}
}

func TestParseLEEFPrefersNameThenMessageForTitle(t *testing.T) {
	named, err := ParseLEEF("LEEF:1.0|IBM|QRadar|1.0|9876|name=Disk failure\tsrc=10.1.1.1\tsev=8\tmsg=disk full on host", "")
	if err != nil {
		t.Fatalf("parse named LEEF: %v", err)
	}
	if named.Title != "Disk failure" {
		t.Fatalf("title=%q want Disk failure", named.Title)
	}
	if named.Severity != "critical" {
		t.Fatalf("severity=%q want critical", named.Severity)
	}
	if named.Content != "disk full on host" {
		t.Fatalf("content=%q", named.Content)
	}
	if named.ExtraFields["src_ip"] != "10.1.1.1" {
		t.Fatalf("extra=%v", named.ExtraFields)
	}
	if named.Source != "IBM/QRadar" {
		t.Fatalf("source=%q", named.Source)
	}

	// 空格分隔且值自带空格的 LEEF：未提供 name 时用完整 msg 作为标题，
	// 且 msg 不会被截成半个词（历史缺陷：标题退化成 EventID "200"）。
	spaceSeparated, err := ParseLEEF("LEEF:2.0|Int2|Probe|1.0|200|src=10.0.0.8 dst=10.0.0.2 sev=5 msg=LEEF probe event", "int2")
	if err != nil {
		t.Fatalf("parse space separated LEEF: %v", err)
	}
	if spaceSeparated.Title != "LEEF probe event" {
		t.Fatalf("title=%q want full msg", spaceSeparated.Title)
	}
	if spaceSeparated.Content != "LEEF probe event" {
		t.Fatalf("content=%q want full msg", spaceSeparated.Content)
	}
	if spaceSeparated.Severity != "medium" {
		t.Fatalf("severity=%q want medium", spaceSeparated.Severity)
	}
}

func TestParseLEEFFallsBackToEventIDAndInfoSeverity(t *testing.T) {
	alert, err := ParseLEEF("LEEF:1.0|Vendor|Product|1.0|42|", "")
	if err != nil {
		t.Fatalf("parse LEEF: %v", err)
	}
	if alert.Title != "42" {
		t.Fatalf("title=%q want event id fallback", alert.Title)
	}
	if alert.Severity != "info" {
		t.Fatalf("severity=%q want info", alert.Severity)
	}
}
