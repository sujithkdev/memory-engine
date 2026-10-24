package search_test

import (
	"strings"
	"testing"

	"github.com/memory-engine/memory-engine/internal/domain"
	"github.com/memory-engine/memory-engine/internal/search"
)

// ── ParseQuery AND ────────────────────────────────────────────────────────────

func TestParseQuery_AND_MultipleTerms(t *testing.T) {
	q := search.ParseQuery("python scripting", domain.SearchAnd)
	if !strings.Contains(q.TSQuery, "&") {
		t.Errorf("AND query should contain &, got: %q", q.TSQuery)
	}
	if !strings.Contains(q.TSQuery, "python") {
		t.Errorf("expected 'python' in query, got: %q", q.TSQuery)
	}
	if !strings.Contains(q.TSQuery, "scripting") {
		t.Errorf("expected 'scripting' in query, got: %q", q.TSQuery)
	}
}

func TestParseQuery_AND_SingleTerm(t *testing.T) {
	q := search.ParseQuery("python", domain.SearchAnd)
	if q.TSQuery != "python" {
		t.Errorf("single term AND: expected 'python', got %q", q.TSQuery)
	}
}

func TestParseQuery_AND_Empty(t *testing.T) {
	q := search.ParseQuery("", domain.SearchAnd)
	if q.TSQuery != "" {
		t.Errorf("empty query should produce empty TSQuery, got %q", q.TSQuery)
	}
}

// ── ParseQuery OR ─────────────────────────────────────────────────────────────

func TestParseQuery_OR_ProducesOrOperator(t *testing.T) {
	q := search.ParseQuery("python OR go OR rust", domain.SearchOr)
	if !strings.Contains(q.TSQuery, "|") {
		t.Errorf("OR query should contain |, got: %q", q.TSQuery)
	}
}

func TestParseQuery_OR_AllTermsPresent(t *testing.T) {
	q := search.ParseQuery("python OR go", domain.SearchOr)
	if !strings.Contains(q.TSQuery, "python") {
		t.Errorf("missing 'python' in OR query: %q", q.TSQuery)
	}
	if !strings.Contains(q.TSQuery, "go") {
		t.Errorf("missing 'go' in OR query: %q", q.TSQuery)
	}
}

// ── ParseQuery PHRASE ─────────────────────────────────────────────────────────

func TestParseQuery_Phrase_WrapsInQuotes(t *testing.T) {
	q := search.ParseQuery("python scripting", domain.SearchPhrase)
	// phraseto_tsquery receives raw text — no manual quoting needed.
	if q.TSQuery == "" {
		t.Errorf("PHRASE query should have non-empty TSQuery, got: %q", q.TSQuery)
	}
	if q.QueryType != domain.SearchPhrase {
		t.Errorf("expected QueryType PHRASE, got %s", q.QueryType)
	}
}

func TestParseQuery_Phrase_ContentPreserved(t *testing.T) {
	q := search.ParseQuery("python scripting", domain.SearchPhrase)
	if !strings.Contains(q.TSQuery, "python") {
		t.Errorf("phrase content not preserved: %q", q.TSQuery)
	}
}

// ── ParseQuery PREFIX ─────────────────────────────────────────────────────────

func TestParseQuery_Prefix_LastTermHasStar(t *testing.T) {
	q := search.ParseQuery("pyth", domain.SearchPrefix)
	if !strings.Contains(q.TSQuery, ":*") {
		t.Errorf("PREFIX query should contain :*, got: %q", q.TSQuery)
	}
}

func TestParseQuery_Prefix_MultiTerm_OnlyLastHasStar(t *testing.T) {
	q := search.ParseQuery("python scri", domain.SearchPrefix)
	if !strings.HasSuffix(strings.TrimSpace(q.TSQuery), ":*") {
		t.Errorf("only last term should have :*, got: %q", q.TSQuery)
	}
}

// ── ParseQuery WEB ────────────────────────────────────────────────────────────

func TestParseQuery_Web_RequiredTermIncluded(t *testing.T) {
	q := search.ParseQuery("+python scripting", domain.SearchWebStyle)
	if !strings.Contains(q.TSQuery, "python") {
		t.Errorf("required term 'python' missing: %q", q.TSQuery)
	}
}

func TestParseQuery_Web_ExcludedTermNegated(t *testing.T) {
	q := search.ParseQuery("python -java", domain.SearchWebStyle)
	if !strings.Contains(q.TSQuery, "!java") {
		t.Errorf("excluded term should be negated with !, got: %q", q.TSQuery)
	}
}

// ── ParseQueryAuto ────────────────────────────────────────────────────────────

func TestParseQueryAuto_QuotedString_DetectsPhrase(t *testing.T) {
	q := search.ParseQueryAuto(`"python scripting"`)
	if q.QueryType != domain.SearchPhrase {
		t.Errorf("quoted string should be PHRASE, got %s", q.QueryType)
	}
}

func TestParseQueryAuto_OrKeyword_DetectsOr(t *testing.T) {
	q := search.ParseQueryAuto("python OR go")
	if q.QueryType != domain.SearchOr {
		t.Errorf("string with OR should be OR type, got %s", q.QueryType)
	}
}

func TestParseQueryAuto_StarSuffix_DetectsPrefix(t *testing.T) {
	q := search.ParseQueryAuto("pyth*")
	if q.QueryType != domain.SearchPrefix {
		t.Errorf("star suffix should be PREFIX, got %s", q.QueryType)
	}
}

func TestParseQueryAuto_PlusPrefix_DetectsWeb(t *testing.T) {
	q := search.ParseQueryAuto("+python -java")
	if q.QueryType != domain.SearchWebStyle {
		t.Errorf("plus/minus prefixes should be WEB, got %s", q.QueryType)
	}
}

func TestParseQueryAuto_Plain_DetectsAnd(t *testing.T) {
	q := search.ParseQueryAuto("python scripting")
	if q.QueryType != domain.SearchAnd {
		t.Errorf("plain text should be AND, got %s", q.QueryType)
	}
}

func TestParseQueryAuto_Empty_ReturnsEmptyTSQuery(t *testing.T) {
	q := search.ParseQueryAuto("")
	if q.TSQuery != "" {
		t.Errorf("empty query should have empty TSQuery, got %q", q.TSQuery)
	}
}

// ── Raw preservation ──────────────────────────────────────────────────────────

func TestParseQuery_RawPreserved(t *testing.T) {
	raw := "python scripting"
	q := search.ParseQuery(raw, domain.SearchAnd)
	if q.Raw != raw {
		t.Errorf("raw query not preserved: expected %q, got %q", raw, q.Raw)
	}
}

func TestParseQuery_QueryTypeSet(t *testing.T) {
	q := search.ParseQuery("python", domain.SearchOr)
	if q.QueryType != domain.SearchOr {
		t.Errorf("expected QueryType=OR, got %s", q.QueryType)
	}
}

// ── Edge cases ────────────────────────────────────────────────────────────────

func TestParseQuery_Punctuation_Stripped(t *testing.T) {
	q := search.ParseQuery("python, scripting!", domain.SearchAnd)
	if strings.Contains(q.TSQuery, ",") || strings.Contains(q.TSQuery, "!") {
		t.Errorf("punctuation should be stripped from query: %q", q.TSQuery)
	}
}

func TestParseQuery_Whitespace_Normalised(t *testing.T) {
	q1 := search.ParseQuery("python  scripting", domain.SearchAnd)
	q2 := search.ParseQuery("python scripting", domain.SearchAnd)
	if q1.TSQuery != q2.TSQuery {
		t.Errorf("extra whitespace should not affect query: %q vs %q", q1.TSQuery, q2.TSQuery)
	}
}
