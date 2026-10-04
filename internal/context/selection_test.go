package context

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSelectForTaskUsesDeliveryLanesAndRelevance(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	always := selectionRecord(t, "Global policy", "Run tests before completion", DeliveryAlways)
	relevant := selectionRecord(t, "Authentication decision", "Use rotating access token", DeliveryRanked)
	unrelated := selectionRecord(t, "CSS notes", "Spacing and typography", DeliveryRanked)
	manual := selectionRecord(t, "Private rollout runbook", "Manual operations", DeliveryManual)

	selection, err := SelectForTask(
		[]ProjectContextRecord{unrelated, manual, relevant, always},
		TaskContextQuery{Title: "Implement authentication token rotation", ExplicitRecordIDs: []string{manual.ID}},
		SelectionOptions{Now: now},
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := selectedIDs(selection.Records); !reflect.DeepEqual(got, []string{always.ID, manual.ID, relevant.ID}) {
		t.Fatalf("selected IDs = %v", got)
	}
	if selection.Records[0].Lane != LaneAlways || selection.Records[1].Lane != LaneExplicit || selection.Records[2].Lane != LaneRanked {
		t.Fatalf("selection lanes = %#v", selection.Records)
	}
	if selection.Records[2].Score <= 0 || !reflect.DeepEqual(selection.Records[2].MatchedTerms, []string{"authentication", "token"}) {
		t.Fatalf("ranked metadata = %#v", selection.Records[2])
	}
	if selection.Metadata.SelectedCount != 3 || selection.Metadata.Usage.DurableBytes <= 0 || selection.Metadata.Warnings == nil {
		t.Fatalf("metadata = %#v", selection.Metadata)
	}
}

func TestSelectForTaskIsDeterministicAndExcludesLifecycleRecords(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	first := selectionRecord(t, "Database migration", "Migrate database schema", DeliveryRanked)
	second := selectionRecord(t, "Database constraints", "Database schema constraints", DeliveryRanked)
	expired := selectionRecord(t, "Old database note", "Database", DeliveryAlways)
	expired.ExpiresAt = timePointer(now)
	superseded := selectionRecord(t, "Replaced database note", "Database", DeliveryAlways)
	superseded.SupersededBy = &first.ID

	query := TaskContextQuery{Title: "Database schema migration"}
	left, err := SelectForTask([]ProjectContextRecord{first, expired, second, superseded}, query, SelectionOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	right, err := SelectForTask([]ProjectContextRecord{superseded, second, expired, first}, query, SelectionOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(selectedIDs(left.Records), selectedIDs(right.Records)) {
		t.Fatalf("selection order changed: %v vs %v", selectedIDs(left.Records), selectedIDs(right.Records))
	}
	if left.Metadata.TaskQueryHash != right.Metadata.TaskQueryHash || left.Metadata.CandidateSetHash != right.Metadata.CandidateSetHash {
		t.Fatalf("assembly hashes changed: %#v vs %#v", left.Metadata, right.Metadata)
	}
	warnings := strings.Join(left.Metadata.Warnings, "\n")
	if !strings.Contains(warnings, "expired") || !strings.Contains(warnings, "superseded") {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestSelectForTaskFailsWhenRequiredContextExceedsBudget(t *testing.T) {
	record := selectionRecord(t, "Required", strings.Repeat("x", 100), DeliveryAlways)
	_, err := SelectForTask([]ProjectContextRecord{record}, TaskContextQuery{}, SelectionOptions{
		Now:                time.Now().UTC(),
		MaxDurableBytes:    32,
		MaxDurableItems:    1,
		MaxRecordBytes:     200,
		AlwaysWarningBytes: 16,
	})
	if ErrorCode(err) != CodeConflict {
		t.Fatalf("error = %v", err)
	}
}

func TestSelectForTaskExpandsDefaultItemBudgetForRequiredAlwaysContext(t *testing.T) {
	records := make([]ProjectContextRecord, 0, 15)
	for index := 0; index < 15; index++ {
		records = append(records, selectionRecord(t, "Required", strings.Repeat("x", 100), DeliveryAlways))
	}

	options := DefaultSelectionOptions()
	selection, err := SelectForTask(records, TaskContextQuery{}, options)
	if err != nil {
		t.Fatal(err)
	}

	metadata := selection.Metadata
	if len(selection.Records) != 15 || metadata.SelectedCount != 15 || metadata.RequiredAlwaysItems != 15 {
		t.Fatalf("selection = %#v", selection)
	}
	if metadata.DefaultBudgetItems != 12 || metadata.DurableBudgetItems != 15 || metadata.ItemBudgetReason != ItemBudgetReasonRequiredAlways {
		t.Fatalf("item budget metadata = %#v", metadata)
	}
	if metadata.Usage.DurableBytes >= metadata.DurableBudgetBytes {
		t.Fatalf("durable usage = %d/%d", metadata.Usage.DurableBytes, metadata.DurableBudgetBytes)
	}
}

func TestSelectForTaskRejectsExplicitItemLimitBelowRequiredAlwaysContext(t *testing.T) {
	records := make([]ProjectContextRecord, 0, 15)
	for index := 0; index < 15; index++ {
		records = append(records, selectionRecord(t, "Required", "small", DeliveryAlways))
	}

	options := DefaultSelectionOptions()
	options.MaxDurableItems = 12
	options.ExpandAlwaysItems = false
	options.ItemBudgetReason = ItemBudgetReasonExplicit
	_, err := SelectForTask(records, TaskContextQuery{}, options)
	budget := ErrorBudget(err)
	if ErrorCode(err) != CodeConflict || budget == nil {
		t.Fatalf("error = %v", err)
	}
	if budget.Reason != BudgetReasonDurableItems || budget.RequiredItems != 15 || budget.MaxItems != 12 || budget.SuggestedContextLimit != 15 || len(budget.RecordIDs) != 15 || budget.Action == "" {
		t.Fatalf("budget error = %#v", budget)
	}
}

func TestSelectForTaskReportsByteAndAbsoluteItemFailuresSeparately(t *testing.T) {
	byteRecords := make([]ProjectContextRecord, 0, 15)
	for index := 0; index < 15; index++ {
		byteRecords = append(byteRecords, selectionRecord(t, "Required", strings.Repeat("x", 1100), DeliveryAlways))
	}

	_, byteErr := SelectForTask(byteRecords, TaskContextQuery{}, DefaultSelectionOptions())
	byteBudget := ErrorBudget(byteErr)
	if byteBudget == nil || byteBudget.Reason != BudgetReasonDurableBytes || byteBudget.RequiredItems != 15 || byteBudget.MaxItems != 15 || byteBudget.RequiredBytes <= byteBudget.MaxBytes {
		t.Fatalf("byte budget error = %#v (%v)", byteBudget, byteErr)
	}

	itemRecords := make([]ProjectContextRecord, 0, DefaultAbsoluteDurableItems+1)
	for index := 0; index <= DefaultAbsoluteDurableItems; index++ {
		itemRecords = append(itemRecords, selectionRecord(t, "Required", "small", DeliveryAlways))
	}

	_, itemErr := SelectForTask(itemRecords, TaskContextQuery{}, DefaultSelectionOptions())
	itemBudget := ErrorBudget(itemErr)
	if itemBudget == nil || itemBudget.Reason != BudgetReasonAbsoluteDurableItems || itemBudget.RequiredItems != 65 || itemBudget.MaxItems != DefaultAbsoluteDurableItems || len(itemBudget.RecordIDs) != 65 {
		t.Fatalf("absolute item budget error = %#v (%v)", itemBudget, itemErr)
	}
}

func TestDiagnoseReportsGrowthAndLifecycleIssues(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	first := selectionRecord(t, "Duplicate", strings.Repeat("a", DefaultAlwaysWarningBytes), DeliveryAlways)
	second := selectionRecord(t, " duplicate ", "b", DeliveryAlways)
	second.ReviewAfter = timePointer(now.Add(-time.Hour))
	second.ExpiresAt = timePointer(now)

	report := Diagnose([]ProjectContextRecord{first, second}, now)
	if report.AlwaysRecords != 2 || report.AlwaysBytes <= DefaultAlwaysWarningBytes || report.ExpiredRecords != 1 || report.ReviewDueRecords != 1 {
		t.Fatalf("report = %#v", report)
	}
	codes := make([]string, 0, len(report.Issues))
	for _, issue := range report.Issues {
		codes = append(codes, issue.Code)
	}
	if !containsString(codes, "always_budget") || !containsString(codes, "duplicate_title") || !containsString(codes, "expired") || !containsString(codes, "review_due") {
		t.Fatalf("issue codes = %v", codes)
	}
}

func BenchmarkSelectForTaskLuchScale(b *testing.B) {
	records := make([]ProjectContextRecord, 0, 59)
	for index := 0; index < 59; index++ {
		delivery := DeliveryRanked
		if index < 4 {
			delivery = DeliveryAlways
		}
		body := strings.Repeat("durable project history ", 45)
		if index%10 == 0 {
			body += " authentication token rotation decision"
		}
		records = append(records, selectionRecord(b, "record", body, delivery))
	}
	query := TaskContextQuery{Title: "Implement authentication token rotation"}
	options := SelectionOptions{Now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}

	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := SelectForTask(records, query, options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSelectForTaskThousandRecords(b *testing.B) {
	records := make([]ProjectContextRecord, 0, 1000)
	for index := 0; index < 1000; index++ {
		delivery := DeliveryRanked
		if index < 5 {
			delivery = DeliveryAlways
		}
		body := "unrelated project history"
		if index%10 == 0 {
			body = "authentication token rotation decision"
		}
		records = append(records, selectionRecord(b, "record", body, delivery))
	}
	query := TaskContextQuery{Title: "Implement authentication token rotation"}
	options := SelectionOptions{Now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}

	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := SelectForTask(records, query, options); err != nil {
			b.Fatal(err)
		}
	}
}

func selectionRecord(t testing.TB, title, body string, delivery Delivery) ProjectContextRecord {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}

	return ProjectContextRecord{
		ID: id, Revision: 1, Kind: KindDecision, Title: title, Body: body, Tags: []string{},
		Source: SourceUser, Visibility: VisibilityShared, Sensitivity: SensitivityNormal, Delivery: delivery,
	}
}

func selectedIDs(values []SelectedRecord) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Record.ID)
	}

	return result
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}

	return false
}
