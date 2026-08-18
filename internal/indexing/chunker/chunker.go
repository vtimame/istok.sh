package chunker

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vtimame/istok.sh/internal/retrieval"
)

const (
	defaultMaxLines    = 80
	defaultMaxBytes    = 8 * 1024
	defaultLineOverlap = 10
	fallbackProvenance = "fallback"
)

// Options configure fallback chunking.
type Options struct {
	MaxLines    int
	MaxBytes    int
	LineOverlap int
}

// SymbolChunk is optional AST-aware metadata.
type SymbolChunk struct {
	Symbol    string
	LineStart int
	LineEnd   int
}

// Chunks returns deterministic chunks for provided file content.
func Chunks(path string, language string, content []byte, contentHash string, symbols []SymbolChunk) ([]retrieval.Chunk, error) {
	return New(Options{}).Chunks(path, language, content, contentHash, symbols)
}

// New creates a chunker with explicit options.
func New(opts Options) *Chunker {
	maxLines := opts.MaxLines
	if maxLines <= 0 {
		maxLines = defaultMaxLines
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if maxBytes < utf8.UTFMax {
		maxBytes = utf8.UTFMax
	}
	lineOverlap := opts.LineOverlap
	if lineOverlap < 0 {
		lineOverlap = 0
	}
	if lineOverlap > defaultLineOverlap {
		lineOverlap = defaultLineOverlap
	}

	return &Chunker{maxLines: maxLines, maxBytes: maxBytes, lineOverlap: lineOverlap}
}

// Chunker applies deterministic line-bounded fallback chunking.
type Chunker struct {
	maxLines    int
	maxBytes    int
	lineOverlap int
}

type lineSpan struct {
	line int
	text string
}

// Chunks splits content into deterministic chunks.
func (c *Chunker) Chunks(path string, language string, content []byte, contentHash string, symbols []SymbolChunk) ([]retrieval.Chunk, error) {
	normalizedPath := retrieval.NormalizePath(path)
	if normalizedPath == "" {
		return nil, fmt.Errorf("empty path")
	}
	if contentHash == "" {
		return nil, fmt.Errorf("empty content hash")
	}
	if !utf8.Valid(content) {
		return nil, fmt.Errorf("content is not valid UTF-8")
	}

	lines := splitLines(content)
	if len(lines) == 0 {
		lines = []lineSpan{{line: 1, text: ""}}
	}

	lineSymbols := buildLineSymbols(symbols)
	chunks := make([]retrieval.Chunk, 0)
	startLine := 0

	for startLine < len(lines) {
		chunkLines := make([]lineSpan, 0, c.maxLines)
		chunkSize := 0
		currentLine := startLine

		for currentLine < len(lines) {
			lineText := lines[currentLine].text

			if len(lineText) > c.maxBytes {
				if len(chunkLines) > 0 {
					break
				}

				chunks = append(chunks, c.chunkLongLine(lines[currentLine], normalizedPath, language, contentHash, lineSymbols)...)
				currentLine++
				break
			}

			nextSize := len(lineText)
			if len(chunkLines) > 0 {
				nextSize++
			}
			if len(chunkLines) > 0 && (len(chunkLines) >= c.maxLines || chunkSize+nextSize > c.maxBytes) {
				break
			}

			chunkLines = append(chunkLines, lines[currentLine])
			chunkSize += nextSize
			currentLine++
		}

		if len(chunkLines) > 0 {
			chunks = append(chunks, buildChunk(normalizedPath, language, contentHash, chunkLines, lineSymbols))
		}

		if currentLine >= len(lines) {
			break
		}

		nextStart := currentLine - c.lineOverlap
		if nextStart <= startLine {
			nextStart = startLine + 1
		}
		startLine = nextStart
	}

	return chunks, nil
}

func (c *Chunker) chunkLongLine(line lineSpan, path, language, contentHash string, lineSymbols map[int][]string) []retrieval.Chunk {
	result := make([]retrieval.Chunk, 0)

	if line.text == "" {
		chunk := retrieval.Chunk{
			ID:                 retrieval.DeterministicChunkID(path, contentHash, line.line, line.line),
			Path:               path,
			Language:           language,
			Content:            "",
			Snippet:            "",
			Identifiers:        []string{},
			Symbol:             firstSymbol(lineSymbols, line.line),
			SymbolNormalized:   strings.ToLower(firstSymbol(lineSymbols, line.line)),
			LineStart:          line.line,
			LineEnd:            line.line,
			ContentHash:        contentHash,
			Provenance:         fallbackProvenance,
			ChunkDiscriminator: "blank",
		}
		result = append(result, chunk)
		return result
	}

	part := 0
	for cursor := 0; cursor < len(line.text); {
		end := cursor + c.maxBytes
		if end > len(line.text) {
			end = len(line.text)
		}
		end = safeUTF8Boundary(line.text, cursor, end)
		if end <= cursor {
			_, size := utf8.DecodeRuneInString(line.text[cursor:])
			if size <= 0 {
				size = 1
			}
			end = cursor + size
			if end > len(line.text) {
				end = len(line.text)
			}
		}

		segment := line.text[cursor:end]
		chunkDiscriminator := retrieval.NormalizePath(strings.Join([]string{"line", strconv.Itoa(line.line), "segment", strconv.Itoa(part)}, "/"))
		result = append(result, retrieval.Chunk{
			ID:                 retrieval.DeterministicChunkID(path, contentHash, line.line, line.line, chunkDiscriminator),
			Path:               path,
			Language:           language,
			Content:            segment,
			Snippet:            segment,
			Identifiers:        retrieval.NormalizeAndExpandIdentifiers(segment),
			Symbol:             firstSymbol(lineSymbols, line.line),
			SymbolNormalized:   strings.ToLower(firstSymbol(lineSymbols, line.line)),
			LineStart:          line.line,
			LineEnd:            line.line,
			ContentHash:        contentHash,
			Provenance:         fallbackProvenance,
			ChunkDiscriminator: chunkDiscriminator,
		})
		cursor = end
		part++
	}

	return result
}

func splitLines(content []byte) []lineSpan {
	if len(content) == 0 {
		return nil
	}

	lines := make([]lineSpan, 0)
	line := 1
	start := 0

	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}

		end := i
		if i > 0 && content[i-1] == '\r' {
			end = i - 1
		}
		lines = append(lines, lineSpan{line: line, text: string(content[start:end])})
		line++
		start = i + 1
	}
	if start <= len(content) {
		lines = append(lines, lineSpan{line: line, text: string(content[start:])})
	}
	return lines
}

func buildChunk(path, language, contentHash string, lines []lineSpan, lineSymbols map[int][]string) retrieval.Chunk {
	startLine := lines[0].line
	endLine := lines[len(lines)-1].line
	content := make([]string, 0, len(lines))
	for _, item := range lines {
		content = append(content, item.text)
	}
	chunkContent := strings.Join(content, "\n")

	return retrieval.Chunk{
		ID:                 retrieval.DeterministicChunkID(path, contentHash, startLine, endLine),
		Path:               path,
		Language:           language,
		Content:            chunkContent,
		Snippet:            chunkContent,
		Identifiers:        retrieval.NormalizeAndExpandIdentifiers(chunkContent),
		Symbol:             symbolForRange(lineSymbols, startLine, endLine),
		SymbolNormalized:   strings.ToLower(symbolForRange(lineSymbols, startLine, endLine)),
		LineStart:          startLine,
		LineEnd:            endLine,
		ContentHash:        contentHash,
		Provenance:         fallbackProvenance,
		ChunkDiscriminator: "",
	}
}

func safeUTF8Boundary(text string, start, end int) int {
	if start < 0 {
		start = 0
	}
	if end >= len(text) {
		return len(text)
	}
	if end <= start {
		return end
	}
	for i := end; i > start; i-- {
		if utf8.ValidString(text[start:i]) {
			return i
		}
	}
	return start + 1
}

func buildLineSymbols(symbols []SymbolChunk) map[int][]string {
	result := make(map[int][]string)
	for _, item := range symbols {
		symbol := strings.TrimSpace(item.Symbol)
		if symbol == "" {
			continue
		}
		if item.LineStart <= 0 || item.LineEnd < item.LineStart {
			continue
		}
		for line := item.LineStart; line <= item.LineEnd; line++ {
			result[line] = append(result[line], symbol)
		}
	}
	for line, values := range result {
		sort.Strings(values)
		result[line] = dedupe(values)
	}
	return result
}

func symbolForRange(symbols map[int][]string, startLine, endLine int) string {
	for line := startLine; line <= endLine; line++ {
		if len(symbols[line]) > 0 {
			return symbols[line][0]
		}
	}
	return ""
}

func firstSymbol(symbols map[int][]string, line int) string {
	if len(symbols[line]) > 0 {
		return symbols[line][0]
	}
	return ""
}

func dedupe(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))
	for _, value := range input {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
