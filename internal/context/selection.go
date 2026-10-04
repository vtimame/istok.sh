package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	AssemblyVersion             = "istok.context-assembly.v1"
	DefaultDurableBudgetBytes   = 16 * 1024
	DefaultRetrievalBudgetBytes = 32 * 1024
	DefaultTotalBudgetBytes     = 48 * 1024
	DefaultAlwaysWarningBytes   = 8 * 1024
	DefaultMaxDurableItems      = 12
	DefaultAbsoluteDurableItems = 64
	DefaultMaxRecordBytes       = 12 * 1024
)

const (
	ItemBudgetReasonDefault        = "default"
	ItemBudgetReasonExplicit       = "explicit"
	ItemBudgetReasonRequiredAlways = "required_always"
	ItemBudgetReasonLegacyAll      = "legacy_all_context"
)

const (
	LaneAlways   = "always"
	LaneExplicit = "explicit"
	LaneRanked   = "ranked"
)

type TaskContextQuery struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Notes              string   `json:"notes"`
	ExplicitRecordIDs  []string `json:"explicit_record_ids"`
}

type SelectionOptions struct {
	Now                time.Time
	MaxDurableBytes    int
	MaxDurableItems    int
	AbsoluteMaxItems   int
	MaxRecordBytes     int
	AlwaysWarningBytes int
	ExpandAlwaysItems  bool
	ItemBudgetReason   string
	IncludeAll         bool
}

func DefaultSelectionOptions() SelectionOptions {
	return SelectionOptions{
		Now:                time.Now().UTC(),
		MaxDurableBytes:    DefaultDurableBudgetBytes,
		MaxDurableItems:    DefaultMaxDurableItems,
		AbsoluteMaxItems:   DefaultAbsoluteDurableItems,
		MaxRecordBytes:     DefaultMaxRecordBytes,
		AlwaysWarningBytes: DefaultAlwaysWarningBytes,
		ExpandAlwaysItems:  true,
		ItemBudgetReason:   ItemBudgetReasonDefault,
	}
}

type SelectedRecord struct {
	Record       ProjectContextRecord
	Lane         string
	Score        float64
	MatchedTerms []string
	Reasons      []string
}

type AssemblyUsage struct {
	AlwaysBytes    int `json:"always_bytes"`
	ExplicitBytes  int `json:"explicit_bytes"`
	RankedBytes    int `json:"ranked_bytes"`
	DurableBytes   int `json:"durable_bytes"`
	RetrievalBytes int `json:"retrieval_bytes"`
	TotalBytes     int `json:"total_bytes"`
}

type AssemblyMetadata struct {
	Version              string        `json:"version"`
	TaskQueryHash        string        `json:"task_query_hash"`
	CandidateSetHash     string        `json:"candidate_set_hash"`
	CandidateCount       int           `json:"candidate_count"`
	SelectedCount        int           `json:"selected_count"`
	DefaultBudgetItems   int           `json:"default_durable_budget_items,omitempty"`
	DurableBudgetItems   int           `json:"durable_budget_items,omitempty"`
	ItemBudgetReason     string        `json:"durable_item_budget_reason,omitempty"`
	RequiredAlwaysItems  int           `json:"required_always_items,omitempty"`
	DurableBudgetBytes   int           `json:"durable_budget_bytes"`
	RetrievalBudgetBytes int           `json:"retrieval_budget_bytes"`
	TotalBudgetBytes     int           `json:"total_budget_bytes"`
	Usage                AssemblyUsage `json:"usage"`
	Warnings             []string      `json:"warnings"`
}

type Selection struct {
	Records  []SelectedRecord
	Metadata AssemblyMetadata
}

func (v AssemblyMetadata) Validate(allowDurableOverride bool) error {
	if v.Version != AssemblyVersion || !validDigest(v.TaskQueryHash) || !validDigest(v.CandidateSetHash) {
		return NewError(CodeInvalid, "context assembly metadata is invalid")
	}
	if v.CandidateCount < 0 || v.SelectedCount < 0 || v.SelectedCount > v.CandidateCount {
		return NewError(CodeInvalid, "context assembly counts are invalid")
	}
	if v.hasItemBudgetMetadata() {
		if v.DefaultBudgetItems <= 0 || v.DurableBudgetItems <= 0 || v.RequiredAlwaysItems < 0 || v.RequiredAlwaysItems > v.SelectedCount {
			return NewError(CodeInvalid, "context assembly item budget metadata is invalid")
		}
		if !validItemBudgetReason(v.ItemBudgetReason) || (!allowDurableOverride && v.SelectedCount > v.DurableBudgetItems) {
			return NewError(CodeInvalid, "context assembly item budget metadata is invalid")
		}
		if v.ItemBudgetReason == ItemBudgetReasonRequiredAlways && (v.DurableBudgetItems != v.RequiredAlwaysItems || v.DurableBudgetItems <= v.DefaultBudgetItems) {
			return NewError(CodeInvalid, "context assembly required always item budget is invalid")
		}
	}
	if v.DurableBudgetBytes <= 0 || v.RetrievalBudgetBytes <= 0 || v.TotalBudgetBytes <= 0 || v.Warnings == nil {
		return NewError(CodeInvalid, "context assembly budget metadata is invalid")
	}
	usage := v.Usage
	if usage.AlwaysBytes < 0 || usage.ExplicitBytes < 0 || usage.RankedBytes < 0 || usage.DurableBytes < 0 || usage.RetrievalBytes < 0 || usage.TotalBytes < 0 {
		return NewError(CodeInvalid, "context assembly usage is invalid")
	}
	if usage.DurableBytes != usage.AlwaysBytes+usage.ExplicitBytes+usage.RankedBytes || usage.TotalBytes != usage.DurableBytes+usage.RetrievalBytes {
		return NewError(CodeInvalid, "context assembly usage totals are inconsistent")
	}
	if usage.RetrievalBytes > v.RetrievalBudgetBytes || (!allowDurableOverride && usage.DurableBytes > v.DurableBudgetBytes) || usage.TotalBytes > v.TotalBudgetBytes {
		return NewError(CodeConflict, "context assembly exceeds its recorded budget")
	}
	for _, warning := range v.Warnings {
		if strings.TrimSpace(warning) == "" {
			return NewError(CodeInvalid, "context assembly warnings must not contain blank values")
		}
	}

	return nil
}

func SelectForTask(records []ProjectContextRecord, query TaskContextQuery, options SelectionOptions) (Selection, error) {
	options.normalize()
	for index, id := range query.ExplicitRecordIDs {
		id = strings.TrimSpace(id)
		if !IsUUIDv7(id) {
			return Selection{}, NewError(CodeInvalid, "explicit context id must be a canonical UUIDv7")
		}
		query.ExplicitRecordIDs[index] = id
	}

	queryHash, err := stableHash(query)
	if err != nil {
		return Selection{}, fmt.Errorf("hash task context query: %w", err)
	}
	candidateHash, err := contextCandidateHash(records)
	if err != nil {
		return Selection{}, fmt.Errorf("hash context candidates: %w", err)
	}

	metadata := AssemblyMetadata{
		Version:              AssemblyVersion,
		TaskQueryHash:        queryHash,
		CandidateSetHash:     candidateHash,
		CandidateCount:       len(records),
		DefaultBudgetItems:   DefaultMaxDurableItems,
		DurableBudgetBytes:   options.MaxDurableBytes,
		RetrievalBudgetBytes: DefaultRetrievalBudgetBytes,
		TotalBudgetBytes:     DefaultTotalBudgetBytes,
		Warnings:             []string{},
	}

	byID := make(map[string]ProjectContextRecord, len(records))
	always := make([]ProjectContextRecord, 0)
	ranked := make([]ProjectContextRecord, 0)
	manual := make([]ProjectContextRecord, 0)
	expired := 0
	superseded := 0
	for _, record := range records {
		if record.DeletedAt != nil || (record.Kind == KindInstruction && (record.Enabled == nil || !*record.Enabled)) {
			continue
		}
		if record.Delivery == "" {
			record.Delivery = DefaultDelivery(record.Kind)
		}
		byID[record.ID] = record
		if record.ExpiresAt != nil && !record.ExpiresAt.After(options.Now) {
			expired++
			continue
		}
		if record.SupersededBy != nil {
			superseded++
			continue
		}

		switch record.Delivery {
		case DeliveryAlways:
			always = append(always, record)
		case DeliveryRanked:
			ranked = append(ranked, record)
		case DeliveryManual:
			manual = append(manual, record)
		default:
			return Selection{}, NewError(CodeInvalid, "context record %s has invalid delivery", record.ID)
		}
	}
	if expired > 0 {
		metadata.Warnings = append(metadata.Warnings, fmt.Sprintf("%d expired context records were excluded", expired))
	}
	if superseded > 0 {
		metadata.Warnings = append(metadata.Warnings, fmt.Sprintf("%d superseded context records were excluded", superseded))
	}

	sort.Slice(always, func(i, j int) bool {
		left, right := instructionPriorityRank(always[i]), instructionPriorityRank(always[j])
		if left != right {
			return left < right
		}
		return always[i].ID < always[j].ID
	})
	alwaysIDs, alwaysBytes := contextRecordIDsAndBytes(always)
	for _, record := range always {
		bytes := recordContentBytes(record)
		if bytes > options.MaxRecordBytes {
			return Selection{}, NewBudgetError(
				fmt.Sprintf("always context record %s requires %d bytes, exceeding the %d-byte per-record budget", record.ID, bytes, options.MaxRecordBytes),
				BudgetFailure{Reason: BudgetReasonRecordBytes, Lane: LaneAlways, Action: "shorten or split the referenced context record", RequiredItems: 1, MaxItems: 1, RequiredBytes: bytes, MaxBytes: options.MaxRecordBytes, RecordIDs: []string{record.ID}},
			)
		}
	}
	if len(always) > options.AbsoluteMaxItems {
		return Selection{}, NewBudgetError(
			fmt.Sprintf("always context requires %d items, exceeding the %d-item absolute durable limit", len(always), options.AbsoluteMaxItems),
			BudgetFailure{Reason: BudgetReasonAbsoluteDurableItems, Lane: LaneAlways, Action: "reduce, merge, or change delivery for always context records", RequiredItems: len(always), MaxItems: options.AbsoluteMaxItems, RequiredBytes: alwaysBytes, MaxBytes: options.MaxDurableBytes, RecordIDs: alwaysIDs},
		)
	}
	if alwaysBytes > options.MaxDurableBytes {
		maxItems := options.MaxDurableItems
		if options.ExpandAlwaysItems {
			maxItems = max(maxItems, len(always))
		}
		return Selection{}, NewBudgetError(
			fmt.Sprintf("always context requires %d bytes, exceeding the %d-byte durable budget", alwaysBytes, options.MaxDurableBytes),
			BudgetFailure{Reason: BudgetReasonDurableBytes, Lane: LaneAlways, Action: "shorten required always context or change nonessential records to ranked delivery", RequiredItems: len(always), MaxItems: maxItems, RequiredBytes: alwaysBytes, MaxBytes: options.MaxDurableBytes, RecordIDs: alwaysIDs},
		)
	}

	effectiveItemBudget := options.MaxDurableItems
	itemBudgetReason := options.ItemBudgetReason
	if len(always) > effectiveItemBudget {
		if !options.ExpandAlwaysItems {
			return Selection{}, NewBudgetError(
				fmt.Sprintf("always context requires %d items, exceeding the explicit %d-item durable limit", len(always), effectiveItemBudget),
				BudgetFailure{Reason: BudgetReasonDurableItems, Lane: LaneAlways, Action: fmt.Sprintf("retry with context_limit=%d or omit the explicit limit", len(always)), RequiredItems: len(always), MaxItems: effectiveItemBudget, RequiredBytes: alwaysBytes, MaxBytes: options.MaxDurableBytes, RecordIDs: alwaysIDs, SuggestedContextLimit: len(always)},
			)
		}

		effectiveItemBudget = len(always)
		itemBudgetReason = ItemBudgetReasonRequiredAlways
		metadata.Warnings = append(metadata.Warnings, fmt.Sprintf("durable item budget expanded from %d to %d for required always context", options.MaxDurableItems, effectiveItemBudget))
	}
	metadata.DurableBudgetItems = effectiveItemBudget
	metadata.ItemBudgetReason = itemBudgetReason
	metadata.RequiredAlwaysItems = len(always)

	selectionCapacity := min(effectiveItemBudget, len(records))
	selected := make([]SelectedRecord, 0, selectionCapacity)
	selectedIDs := make(map[string]struct{})
	usedBytes := 0
	addRequired := func(record ProjectContextRecord, lane string, score float64, matched, reasons []string) error {
		if _, exists := selectedIDs[record.ID]; exists {
			return nil
		}
		bytes := recordContentBytes(record)
		if bytes > options.MaxRecordBytes {
			return NewBudgetError(
				fmt.Sprintf("%s context record %s requires %d bytes, exceeding the %d-byte per-record budget", lane, record.ID, bytes, options.MaxRecordBytes),
				BudgetFailure{Reason: BudgetReasonRecordBytes, Lane: lane, Action: "shorten or split the referenced context record", RequiredItems: 1, MaxItems: 1, RequiredBytes: bytes, MaxBytes: options.MaxRecordBytes, RecordIDs: []string{record.ID}},
			)
		}
		requiredIDs := selectedRecordIDs(selected, record.ID)
		if len(selected) >= options.AbsoluteMaxItems {
			return NewBudgetError(
				fmt.Sprintf("%s context requires %d items, exceeding the %d-item absolute durable limit", lane, len(selected)+1, options.AbsoluteMaxItems),
				BudgetFailure{Reason: BudgetReasonAbsoluteDurableItems, Lane: lane, Action: "reduce, merge, or remove explicit context references", RequiredItems: len(selected) + 1, MaxItems: options.AbsoluteMaxItems, RequiredBytes: usedBytes + bytes, MaxBytes: options.MaxDurableBytes, RecordIDs: requiredIDs},
			)
		}
		if len(selected) >= effectiveItemBudget {
			suggestedLimit := len(selected) + 1
			if suggestedLimit > options.AbsoluteMaxItems {
				suggestedLimit = 0
			}
			return NewBudgetError(
				fmt.Sprintf("%s context requires %d items, exceeding the %d-item durable limit", lane, len(selected)+1, effectiveItemBudget),
				BudgetFailure{Reason: BudgetReasonDurableItems, Lane: lane, Action: durableItemAction(suggestedLimit), RequiredItems: len(selected) + 1, MaxItems: effectiveItemBudget, RequiredBytes: usedBytes + bytes, MaxBytes: options.MaxDurableBytes, RecordIDs: requiredIDs, SuggestedContextLimit: suggestedLimit},
			)
		}
		if usedBytes+bytes > options.MaxDurableBytes {
			return NewBudgetError(
				fmt.Sprintf("%s context requires %d bytes, exceeding the %d-byte durable budget", lane, usedBytes+bytes, options.MaxDurableBytes),
				BudgetFailure{Reason: BudgetReasonDurableBytes, Lane: lane, Action: "shorten required context or remove explicit context references", RequiredItems: len(selected) + 1, MaxItems: effectiveItemBudget, RequiredBytes: usedBytes + bytes, MaxBytes: options.MaxDurableBytes, RecordIDs: requiredIDs},
			)
		}

		selected = append(selected, SelectedRecord{Record: record, Lane: lane, Score: score, MatchedTerms: nonNilStrings(matched), Reasons: nonNilStrings(reasons)})
		selectedIDs[record.ID] = struct{}{}
		usedBytes += bytes
		switch lane {
		case LaneAlways:
			metadata.Usage.AlwaysBytes += bytes
		case LaneExplicit:
			metadata.Usage.ExplicitBytes += bytes
		}

		return nil
	}

	for _, record := range always {
		if err := addRequired(record, LaneAlways, 0, nil, []string{"delivery=always"}); err != nil {
			return Selection{}, err
		}
	}
	if metadata.Usage.AlwaysBytes > options.AlwaysWarningBytes {
		metadata.Warnings = append(metadata.Warnings, fmt.Sprintf("always context uses %d bytes; review the %d-byte soft limit", metadata.Usage.AlwaysBytes, options.AlwaysWarningBytes))
	}

	for _, id := range query.ExplicitRecordIDs {
		record, exists := byID[id]
		if !exists || record.DeletedAt != nil {
			return Selection{}, NewError(CodeNotFound, "explicit context record %s was not found", id)
		}
		if record.ExpiresAt != nil && !record.ExpiresAt.After(options.Now) {
			return Selection{}, NewError(CodeConflict, "explicit context record %s is expired", id)
		}
		if record.SupersededBy != nil {
			return Selection{}, NewError(CodeConflict, "explicit context record %s is superseded by %s", id, *record.SupersededBy)
		}
		if err := addRequired(record, LaneExplicit, 0, nil, []string{"explicit task reference"}); err != nil {
			return Selection{}, err
		}
	}
	if options.IncludeAll {
		all := append(append([]ProjectContextRecord{}, ranked...), manual...)
		sort.Slice(all, func(i, j int) bool {
			if !all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
				return all[i].UpdatedAt.After(all[j].UpdatedAt)
			}
			return all[i].ID < all[j].ID
		})
		for _, record := range all {
			if err := addRequired(record, LaneExplicit, 0, nil, []string{"audited legacy all-context override"}); err != nil {
				return Selection{}, err
			}
		}
	}

	weightedTerms := taskQueryTerms(query)
	type scoredRecord struct {
		record  ProjectContextRecord
		score   float64
		matched []string
	}
	scored := make([]scoredRecord, 0, len(ranked))
	documentFrequency := termDocumentFrequency(ranked, weightedTerms)
	for _, record := range ranked {
		if _, exists := selectedIDs[record.ID]; exists {
			continue
		}
		score, matched := scoreRecord(record, weightedTerms, documentFrequency, len(ranked))
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredRecord{record: record, score: score, matched: matched})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if !scored[i].record.UpdatedAt.Equal(scored[j].record.UpdatedAt) {
			return scored[i].record.UpdatedAt.After(scored[j].record.UpdatedAt)
		}
		return scored[i].record.ID < scored[j].record.ID
	})

	for _, candidate := range scored {
		if len(selected) >= effectiveItemBudget {
			break
		}
		bytes := recordContentBytes(candidate.record)
		if bytes > options.MaxRecordBytes || usedBytes+bytes > options.MaxDurableBytes {
			continue
		}

		selected = append(selected, SelectedRecord{
			Record:       candidate.record,
			Lane:         LaneRanked,
			Score:        candidate.score,
			MatchedTerms: candidate.matched,
			Reasons:      []string{"task lexical relevance"},
		})
		selectedIDs[candidate.record.ID] = struct{}{}
		usedBytes += bytes
		metadata.Usage.RankedBytes += bytes
	}

	for _, item := range selected {
		if item.Record.ReviewAfter != nil && !item.Record.ReviewAfter.After(options.Now) {
			metadata.Warnings = append(metadata.Warnings, fmt.Sprintf("context record %s is due for review", item.Record.ID))
		}
	}
	metadata.SelectedCount = len(selected)
	metadata.Usage.DurableBytes = usedBytes
	metadata.Usage.TotalBytes = usedBytes

	return Selection{Records: selected, Metadata: metadata}, nil
}

func (v *SelectionOptions) normalize() {
	if v.Now.IsZero() {
		v.Now = time.Now().UTC()
	} else {
		v.Now = v.Now.UTC()
	}
	if v.MaxDurableBytes <= 0 {
		v.MaxDurableBytes = DefaultDurableBudgetBytes
	}
	if v.MaxDurableItems <= 0 {
		v.MaxDurableItems = DefaultMaxDurableItems
	}
	if v.AbsoluteMaxItems <= 0 {
		v.AbsoluteMaxItems = DefaultAbsoluteDurableItems
	}
	if v.MaxRecordBytes <= 0 {
		v.MaxRecordBytes = DefaultMaxRecordBytes
	}
	if v.AlwaysWarningBytes <= 0 {
		v.AlwaysWarningBytes = DefaultAlwaysWarningBytes
	}
	if v.ItemBudgetReason == "" {
		v.ItemBudgetReason = ItemBudgetReasonDefault
		v.ExpandAlwaysItems = true
	}
}

func (v AssemblyMetadata) hasItemBudgetMetadata() bool {
	return v.DefaultBudgetItems != 0 || v.DurableBudgetItems != 0 || v.ItemBudgetReason != "" || v.RequiredAlwaysItems != 0
}

func validItemBudgetReason(value string) bool {
	return value == ItemBudgetReasonDefault || value == ItemBudgetReasonExplicit || value == ItemBudgetReasonRequiredAlways || value == ItemBudgetReasonLegacyAll
}

func contextRecordIDsAndBytes(records []ProjectContextRecord) ([]string, int) {
	ids := make([]string, 0, len(records))
	bytes := 0
	for _, record := range records {
		ids = append(ids, record.ID)
		bytes += recordContentBytes(record)
	}

	return ids, bytes
}

func selectedRecordIDs(records []SelectedRecord, additionalID string) []string {
	ids := make([]string, 0, len(records)+1)
	for _, record := range records {
		ids = append(ids, record.Record.ID)
	}
	ids = append(ids, additionalID)

	return ids
}

func durableItemAction(suggestedLimit int) string {
	if suggestedLimit > 0 {
		return fmt.Sprintf("retry with context_limit=%d or remove explicit context references", suggestedLimit)
	}

	return "reduce, merge, or remove explicit context references"
}

func taskQueryTerms(query TaskContextQuery) map[string]float64 {
	result := map[string]float64{}
	addTerms(result, query.Title, 3)
	addTerms(result, query.Description, 2)
	addTerms(result, query.AcceptanceCriteria, 2)
	addTerms(result, query.Notes, 1)

	return result
}

func addTerms(result map[string]float64, value string, weight float64) {
	for _, term := range tokenize(value) {
		result[term] += weight
	}
}

func tokenize(value string) []string {
	terms := make([]string, 0)
	seen := map[string]struct{}{}
	for _, field := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-'
	}) {
		field = strings.Trim(field, "_-")
		if len([]rune(field)) < 2 || stopwords[field] {
			continue
		}
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
	}
	sort.Strings(terms)

	return terms
}

func termDocumentFrequency(records []ProjectContextRecord, terms map[string]float64) map[string]int {
	result := make(map[string]int, len(terms))
	for _, record := range records {
		document := tokenSet(record.Title + " " + strings.Join(record.Tags, " ") + " " + record.Body)
		for term := range terms {
			if document[term] {
				result[term]++
			}
		}
	}

	return result
}

func scoreRecord(record ProjectContextRecord, weighted map[string]float64, frequency map[string]int, documents int) (float64, []string) {
	title := tokenSet(record.Title)
	body := tokenSet(record.Body)
	tags := tokenSet(strings.Join(record.Tags, " "))
	matched := make([]string, 0)
	score := 0.0
	for term, queryWeight := range weighted {
		fieldWeight := 0.0
		if tags[term] {
			fieldWeight += 8
		}
		if title[term] {
			fieldWeight += 4
		}
		if body[term] {
			fieldWeight += 1
		}
		if fieldWeight == 0 {
			continue
		}

		idf := math.Log(float64(documents+1)/float64(frequency[term]+1)) + 1
		score += queryWeight * fieldWeight * idf
		matched = append(matched, term)
	}
	sort.Strings(matched)

	return score * kindWeight(record.Kind), matched
}

func kindWeight(kind Kind) float64 {
	switch kind {
	case KindConstraint:
		return 1.2
	case KindDecision:
		return 1.15
	case KindInstruction:
		return 1.1
	default:
		return 1
	}
}

func tokenSet(value string) map[string]bool {
	result := map[string]bool{}
	for _, term := range tokenize(value) {
		result[term] = true
	}

	return result
}

func instructionPriorityRank(record ProjectContextRecord) int {
	if record.Kind != KindInstruction || record.Priority == nil {
		return 4
	}
	switch *record.Priority {
	case PriorityCritical:
		return 0
	case PriorityHigh:
		return 1
	case PriorityNormal:
		return 2
	default:
		return 3
	}
}

func recordContentBytes(record ProjectContextRecord) int {
	return len(record.Title) + len(record.Body) + len(strings.Join(record.Tags, ","))
}

func contextCandidateHash(records []ProjectContextRecord) (string, error) {
	type candidate struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	values := make([]candidate, 0, len(records))
	for _, record := range records {
		values = append(values, candidate{ID: record.ID, Revision: record.Revision})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })

	return stableHash(values)
}

func stableHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:]), nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)

	return err == nil
}

func nonNilStrings(values []string) []string {
	return append([]string{}, values...)
}

var stopwords = map[string]bool{
	"and": true, "are": true, "for": true, "from": true, "into": true, "not": true, "only": true, "the": true, "this": true, "with": true,
	"без": true, "для": true, "или": true, "как": true, "на": true, "не": true, "по": true, "при": true, "с": true, "только": true, "что": true, "это": true,
}
