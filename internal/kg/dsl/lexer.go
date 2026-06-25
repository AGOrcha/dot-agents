package dsl

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// tokenKind enumerates the lexical token classes of the v1 DSL grammar (§5.1).
type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokParam  // $name
	tokString // 'literal'
	tokNumber // 123 or 1.5
	tokPunct  // ( ) [ ] . , : - > * = ! < AND etc. — see below
	tokOp     // = != < <= > >= (resolved punct runs)
)

// token is one lexeme with its source position (for error messages).
type token struct {
	kind tokenKind
	text string
	pos  int
}

// keywordSet is the reserved-word set the lexer surfaces as upper-cased idents
// so the parser can match clauses case-insensitively while field names stay
// case-sensitive.
var keywordSet = map[string]bool{
	"MATCH": true, "OPTIONAL": true, "WHERE": true, "RETURN": true,
	"AND": true, "IN": true, "AS": true,
}

// lexer turns a DSL source string into tokens. It is single-pass and produces a
// flat token slice the recursive-descent parser consumes.
type lexer struct {
	src    string
	pos    int
	tokens []token
}

// lex tokenizes src or returns the first lexical error encountered.
func lex(src string) ([]token, error) {
	l := &lexer{src: src}
	if err := l.run(); err != nil {
		return nil, err
	}
	l.tokens = append(l.tokens, token{kind: tokEOF, pos: l.pos})
	return l.tokens, nil
}

// run scans the whole source, dispatching one lexeme per loop turn.
func (l *lexer) run() error {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case c == '\'':
			if err := l.lexString(); err != nil {
				return err
			}
		case c == '$':
			l.lexParam()
		case isIdentStart(rune(c)):
			l.lexIdent()
		case unicode.IsDigit(rune(c)) || (c == '-' && l.nextIsDigit()):
			l.lexNumber()
		default:
			if err := l.lexPunct(); err != nil {
				return err
			}
		}
	}
	return nil
}

// nextIsDigit reports whether the char after the current one is a digit (used
// to disambiguate a negative number from the `->` arrow / `-` punct).
func (l *lexer) nextIsDigit() bool {
	return l.pos+1 < len(l.src) && unicode.IsDigit(rune(l.src[l.pos+1]))
}

func (l *lexer) lexString() error {
	start := l.pos
	l.pos++ // opening quote
	for l.pos < len(l.src) && l.src[l.pos] != '\'' {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return fmt.Errorf("dsl: unterminated string literal at offset %d", start)
	}
	text := l.src[start+1 : l.pos]
	l.pos++ // closing quote
	l.tokens = append(l.tokens, token{kind: tokString, text: text, pos: start})
	return nil
}

func (l *lexer) lexParam() {
	start := l.pos
	l.pos++ // $
	for l.pos < len(l.src) && isIdentPart(rune(l.src[l.pos])) {
		l.pos++
	}
	l.tokens = append(l.tokens, token{kind: tokParam, text: l.src[start+1 : l.pos], pos: start})
}

func (l *lexer) lexIdent() {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(rune(l.src[l.pos])) {
		l.pos++
	}
	text := l.src[start:l.pos]
	if up := strings.ToUpper(text); keywordSet[up] {
		text = up
	}
	l.tokens = append(l.tokens, token{kind: tokIdent, text: text, pos: start})
}

func (l *lexer) lexNumber() {
	start := l.pos
	if l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.src) && unicode.IsDigit(rune(l.src[l.pos])) {
		l.pos++
	}
	// A single decimal point with a following digit extends the number (1.5);
	// a `..` range separator (variable-length pattern) does NOT — stop before
	// it so `*1..3` lexes as 1, ., ., 3.
	if l.pos+1 < len(l.src) && l.src[l.pos] == '.' && unicode.IsDigit(rune(l.src[l.pos+1])) {
		l.pos++
		for l.pos < len(l.src) && unicode.IsDigit(rune(l.src[l.pos])) {
			l.pos++
		}
	}
	l.tokens = append(l.tokens, token{kind: tokNumber, text: l.src[start:l.pos], pos: start})
}

// twoCharOps are the multi-char operators resolved before single chars.
var twoCharOps = map[string]bool{"!=": true, "<=": true, ">=": true, "->": true, "<>": true}

// lexPunct emits a punctuation or operator token. Two-char sequences (!=, <=,
// >=, ->, <>) are matched before single chars; `<>` is intentionally tokenized
// so the parser can reject it with a precise message (T39) rather than the
// lexer failing opaquely.
func (l *lexer) lexPunct() error {
	if l.pos+1 < len(l.src) {
		two := l.src[l.pos : l.pos+2]
		if twoCharOps[two] {
			l.tokens = append(l.tokens, token{kind: tokOp, text: two, pos: l.pos})
			l.pos += 2
			return nil
		}
	}
	c := l.src[l.pos]
	if !strings.ContainsRune("()[].,:->*=!<", rune(c)) {
		return fmt.Errorf("dsl: unexpected character %q at offset %d", string(c), l.pos)
	}
	l.tokens = append(l.tokens, token{kind: tokPunct, text: string(c), pos: l.pos})
	l.pos++
	return nil
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// parseNumber converts a numeric token's text to an int or float64.
func parseNumber(text string) (any, error) {
	if !strings.Contains(text, ".") {
		if n, err := strconv.Atoi(text); err == nil {
			return n, nil
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, fmt.Errorf("dsl: invalid number %q", text)
	}
	return f, nil
}
