package context

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type DiagnosticIssue struct {
	Severity              string   `json:"severity"`
	Code                  string   `json:"code"`
	Message               string   `json:"message"`
	Action                string   `json:"action,omitempty"`
	RequiredItems         int      `json:"required_items,omitempty"`
	MaxItems              int      `json:"max_items,omitempty"`
	RequiredBytes         int      `json:"required_bytes,omitempty"`
	MaxBytes              int      `json:"max_bytes,omitempty"`
	SuggestedContextLimit int      `json:"suggested_context_limit,omitempty"`
	RecordIDs             []string `json:"record_ids"`
}

type DiagnosticReport struct {
	ActiveRecords     int               `json:"active_records"`
	ActiveBytes       int               `json:"active_bytes"`
	AlwaysRecords     int               `json:"always_records"`
	AlwaysBytes       int               `json:"always_bytes"`
	RankedRecords     int               `json:"ranked_records"`
	ManualRecords     int               `json:"manual_records"`
	ExpiredRecords    int               `json:"expired_records"`
	SupersededRecords int               `json:"superseded_records"`
	ReviewDueRecords  int               `json:"review_due_records"`
	Issues            []DiagnosticIssue `json:"issues"`
}

func Diagnose(records []ProjectContextRecord, now time.Time) DiagnosticReport {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	report := DiagnosticReport{Issues: []DiagnosticIssue{}}
	byTitle := map[string][]string{}
	alwaysIDs := []string{}
	requiredAlwaysRecords := 0
	requiredAlwaysBytes := 0
	for _, record := range records {
		if record.DeletedAt != nil || (record.Kind == KindInstruction && (record.Enabled == nil || !*record.Enabled)) {
			continue
		}
		report.ActiveRecords++
		bytes := recordContentBytes(record)
		report.ActiveBytes += bytes
		expired := record.ExpiresAt != nil && !record.ExpiresAt.After(now)
		superseded := record.SupersededBy != nil
		if bytes > DefaultMaxRecordBytes && !expired && !superseded {
			report.Issues = append(report.Issues, DiagnosticIssue{Severity: "error", Code: "oversized_record", Message: fmt.Sprintf("context record uses %d bytes, above the %d-byte per-record limit", bytes, DefaultMaxRecordBytes), Action: "shorten or split the context record", RequiredItems: 1, MaxItems: 1, RequiredBytes: bytes, MaxBytes: DefaultMaxRecordBytes, RecordIDs: []string{record.ID}})
		}
		delivery := record.Delivery
		if delivery == "" {
			delivery = DefaultDelivery(record.Kind)
		}
		switch delivery {
		case DeliveryAlways:
			report.AlwaysRecords++
			report.AlwaysBytes += bytes
			if !expired && !superseded {
				requiredAlwaysRecords++
				requiredAlwaysBytes += bytes
				alwaysIDs = append(alwaysIDs, record.ID)
			}
		case DeliveryRanked:
			report.RankedRecords++
		case DeliveryManual:
			report.ManualRecords++
		}
		if expired {
			report.ExpiredRecords++
			report.Issues = append(report.Issues, DiagnosticIssue{Severity: "warning", Code: "expired", Message: "context record is expired and excluded from automatic assembly", RecordIDs: []string{record.ID}})
		}
		if superseded {
			report.SupersededRecords++
		}
		if record.ReviewAfter != nil && !record.ReviewAfter.After(now) {
			report.ReviewDueRecords++
			report.Issues = append(report.Issues, DiagnosticIssue{Severity: "warning", Code: "review_due", Message: "context record is due for review", RecordIDs: []string{record.ID}})
		}
		title := strings.ToLower(strings.TrimSpace(record.Title))
		if title != "" {
			byTitle[title] = append(byTitle[title], record.ID)
		}
	}
	sort.Strings(alwaysIDs)
	if requiredAlwaysRecords > DefaultAbsoluteDurableItems {
		report.Issues = append(report.Issues, DiagnosticIssue{Severity: "error", Code: "always_absolute_item_budget", Message: fmt.Sprintf("always context requires %d items, above the %d-item absolute durable limit", requiredAlwaysRecords, DefaultAbsoluteDurableItems), Action: "reduce, merge, or change delivery for always context records", RequiredItems: requiredAlwaysRecords, MaxItems: DefaultAbsoluteDurableItems, RequiredBytes: requiredAlwaysBytes, MaxBytes: DefaultDurableBudgetBytes, RecordIDs: alwaysIDs})
	} else if requiredAlwaysRecords > DefaultMaxDurableItems {
		report.Issues = append(report.Issues, DiagnosticIssue{Severity: "warning", Code: "always_item_budget_expansion", Message: fmt.Sprintf("always context will expand the default item budget from %d to %d", DefaultMaxDurableItems, requiredAlwaysRecords), Action: fmt.Sprintf("omit context_limit or set it to at least %d", requiredAlwaysRecords), RequiredItems: requiredAlwaysRecords, MaxItems: DefaultMaxDurableItems, RequiredBytes: requiredAlwaysBytes, MaxBytes: DefaultDurableBudgetBytes, SuggestedContextLimit: requiredAlwaysRecords, RecordIDs: alwaysIDs})
	}
	if requiredAlwaysBytes > DefaultDurableBudgetBytes {
		report.Issues = append(report.Issues, DiagnosticIssue{Severity: "error", Code: "always_byte_budget", Message: fmt.Sprintf("always context uses %d bytes, above the %d-byte durable limit", requiredAlwaysBytes, DefaultDurableBudgetBytes), Action: "shorten required always context or change nonessential records to ranked delivery", RequiredItems: requiredAlwaysRecords, MaxItems: DefaultAbsoluteDurableItems, RequiredBytes: requiredAlwaysBytes, MaxBytes: DefaultDurableBudgetBytes, RecordIDs: alwaysIDs})
	} else if requiredAlwaysBytes > DefaultAlwaysWarningBytes {
		report.Issues = append(report.Issues, DiagnosticIssue{Severity: "warning", Code: "always_budget", Message: fmt.Sprintf("always context uses %d bytes, above the %d-byte soft limit", requiredAlwaysBytes, DefaultAlwaysWarningBytes), Action: "review always context and move nonessential records to ranked delivery", RequiredItems: requiredAlwaysRecords, MaxItems: DefaultAbsoluteDurableItems, RequiredBytes: requiredAlwaysBytes, MaxBytes: DefaultAlwaysWarningBytes, RecordIDs: alwaysIDs})
	}
	for _, ids := range byTitle {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		report.Issues = append(report.Issues, DiagnosticIssue{Severity: "info", Code: "duplicate_title", Message: "multiple active context records have the same normalized title", RecordIDs: ids})
	}
	sort.SliceStable(report.Issues, func(i, j int) bool {
		if report.Issues[i].Severity != report.Issues[j].Severity {
			return report.Issues[i].Severity < report.Issues[j].Severity
		}
		if report.Issues[i].Code != report.Issues[j].Code {
			return report.Issues[i].Code < report.Issues[j].Code
		}
		return strings.Join(report.Issues[i].RecordIDs, "\x00") < strings.Join(report.Issues[j].RecordIDs, "\x00")
	})

	return report
}
