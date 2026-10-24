// Package search implements full-text search query parsing and execution.
package search

import (
	"strings"

	"github.com/memory-engine/memory-engine/internal/domain"
)

// ParseQuery converts a user-supplied search string into a PostgreSQL tsquery.
//
// Supported query types:
//
//	AND (default) — all terms must match: "python scripting" → "python & scripting"
//	OR            — any term matches:     "python OR go"     → "python | go"
//	PHRASE        — exact phrase:         "python scripting" → "'python scripting'"
//	PREFIX        — prefix match:         "pyth"             → "pyth:*"
//	WEB           — +required -excluded:  "+python -java"    → "python & !java"
func ParseQuery(raw string, queryType domain.SearchQueryType) domain.SearchQuery {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return domain.SearchQuery{Raw: raw, TSQuery: "", QueryType: queryType}
	}

	var tsQuery string

	switch queryType {
	case domain.SearchPhrase:
		tsQuery = buildPhraseQuery(raw)
	case domain.SearchOr:
		tsQuery = buildOrQuery(raw)
	case domain.SearchPrefix:
		tsQuery = buildPrefixQuery(raw)
	case domain.SearchWebStyle:
		tsQuery = buildWebQuery(raw)
	default:
		// Default: AND query.
		queryType = domain.SearchAnd
		tsQuery = buildAndQuery(raw)
	}

	return domain.SearchQuery{
		Raw:       raw,
		TSQuery:   tsQuery,
		QueryType: queryType,
	}
}

// ParseQueryAuto detects the query type from the raw string automatically.
//
//   - Quoted string → PHRASE
//   - Contains +/- prefixes → WEB
//   - Ends with * → PREFIX
//   - Contains " OR " → OR
//   - Otherwise → AND
func ParseQueryAuto(raw string) domain.SearchQuery {
	raw = strings.TrimSpace(raw)

	switch {
	case strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\""):
		return ParseQuery(raw[1:len(raw)-1], domain.SearchPhrase)
	case strings.Contains(raw, "+") || (strings.Contains(raw, "-") && !strings.HasPrefix(raw, "-")):
		return ParseQuery(raw, domain.SearchWebStyle)
	case strings.HasSuffix(raw, "*"):
		return ParseQuery(strings.TrimSuffix(raw, "*"), domain.SearchPrefix)
	case strings.Contains(strings.ToUpper(raw), " OR "):
		return ParseQuery(raw, domain.SearchOr)
	default:
		return ParseQuery(raw, domain.SearchAnd)
	}
}

// ── Query builders ────────────────────────────────────────────────────────────

// buildAndQuery: "python scripting" → "python & scripting"
func buildAndQuery(raw string) string {
	terms := tokenise(raw)
	if len(terms) == 0 {
		return ""
	}
	escaped := escapeTerms(terms)
	return strings.Join(escaped, " & ")
}

// buildOrQuery: "python go rust" → "python | go | rust"
func buildOrQuery(raw string) string {
	// Split on " OR " first, then tokenise remaining.
	parts := strings.Split(raw, " OR ")
	var escaped []string
	for _, p := range parts {
		terms := tokenise(p)
		escaped = append(escaped, escapeTerms(terms)...)
	}
	if len(escaped) == 0 {
		return ""
	}
	return strings.Join(escaped, " | ")
}

// buildPhraseQuery: passes raw content — phraseto_tsquery handles it natively.
func buildPhraseQuery(raw string) string {
	// phraseto_tsquery takes plain text, no special operators needed.
	return raw
}

// buildPrefixQuery: "pyth" → "pyth:*"
func buildPrefixQuery(raw string) string {
	terms := tokenise(raw)
	if len(terms) == 0 {
		return ""
	}
	// Apply :* to the last term (the one being typed).
	result := make([]string, len(terms))
	for i, t := range terms {
		if i == len(terms)-1 {
			result[i] = sanitiseTerm(t) + ":*"
		} else {
			result[i] = sanitiseTerm(t)
		}
	}
	return strings.Join(result, " & ")
}

// buildWebQuery: "+python -java scripting" → "python & !java & scripting"
func buildWebQuery(raw string) string {
	tokens := strings.Fields(raw)
	var required, excluded, optional []string

	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if strings.HasPrefix(tok, "+") {
			term := sanitiseTerm(strings.TrimPrefix(tok, "+"))
			if term != "" {
				required = append(required, term)
			}
		} else if strings.HasPrefix(tok, "-") {
			term := sanitiseTerm(strings.TrimPrefix(tok, "-"))
			if term != "" {
				excluded = append(excluded, "!"+term)
			}
		} else {
			term := sanitiseTerm(tok)
			if term != "" {
				optional = append(optional, term)
			}
		}
	}

	all := append(append(required, excluded...), optional...)
	if len(all) == 0 {
		return ""
	}
	return strings.Join(all, " & ")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// tokenise splits raw input into individual search terms.
// Strips punctuation, skips stop words, lowercases.
func tokenise(raw string) []string {
	raw = strings.ToLower(raw)
	fields := strings.Fields(raw)
	var terms []string
	for _, f := range fields {
		f = strings.Trim(f, `.,!?;:"'()[]{}`)
		if f == "" {
			continue
		}
		terms = append(terms, f)
	}
	return terms
}

// escapeTerms sanitises a slice of terms for tsquery.
func escapeTerms(terms []string) []string {
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		s := sanitiseTerm(t)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// sanitiseTerm removes characters not valid in a tsquery lexeme.
func sanitiseTerm(t string) string {
	// Remove characters that break tsquery.
	replacer := strings.NewReplacer(
		"'", "", "\"", "", "&", "", "|", "", "!", "", "(", "", ")", "", "<", "", ">", "",
	)
	cleaned := strings.TrimSpace(replacer.Replace(t))
	return cleaned
}
