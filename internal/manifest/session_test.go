package manifest

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// -- SanitizeTitle: single-line normalization (no truncation) ------------

func TestSanitizeTitle_Passthrough(t *testing.T) {
	got := SanitizeTitle("normal single-line title")
	if got != "normal single-line title" {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestSanitizeTitle_CollapsesNewlinesToSpace(t *testing.T) {
	in := "line one\nline two\n\nline four"
	want := "line one line two line four"
	got := SanitizeTitle(in)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeTitle_DropsControlCharsKeepsWhitespace(t *testing.T) {
	// ANSI escape (ESC, a control char) is dropped, but its CSI payload
	// "[31m" is printable ASCII and stays. Tab and form feed are Unicode
	// whitespace — they collapse to a single space. Bell (\x07) is a
	// non-whitespace control char and is dropped entirely (no separator).
	in := "alpha\x1b[31m\tbeta\fgamma\x07delta"
	got := SanitizeTitle(in)
	want := "alpha[31m beta gammadelta"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeTitle_BareEscDropsSilently(t *testing.T) {
	got := SanitizeTitle("before\x1bafter")
	if got != "beforeafter" {
		t.Errorf("got %q, want beforeafter", got)
	}
}

func TestSanitizeTitle_CollapsesRunsOfWhitespace(t *testing.T) {
	in := "  hello   \t\n   world \n\n"
	want := "hello world"
	got := SanitizeTitle(in)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeTitle_EmptyInput(t *testing.T) {
	if got := SanitizeTitle(""); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
	if got := SanitizeTitle("\n\n   \t"); got != "" {
		t.Errorf("whitespace-only input should return empty, got %q", got)
	}
}

func TestSanitizeTitle_CRLF(t *testing.T) {
	got := SanitizeTitle("windows\r\nstyle\r\nlines")
	if got != "windows style lines" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeTitle_DoesNotTruncate(t *testing.T) {
	// Even a very long input must pass through unchanged (save-boundary
	// contract: truncation is the display layer's job).
	in := strings.Repeat("x", 1000)
	got := SanitizeTitle(in)
	if got != in {
		t.Errorf("SanitizeTitle should not truncate; got %d bytes, want 1000", len(got))
	}
}

func TestSanitizeTitle_MixedMultibyte(t *testing.T) {
	in := "hello 🌞\nworld 世界!"
	got := SanitizeTitle(in)
	want := "hello 🌞 world 世界!"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// -- TruncateRunes: display-time, rune-safe truncation ------------------

func TestTruncateRunes_NoTruncationBelowLimit(t *testing.T) {
	if got := TruncateRunes("short", 50); got != "short" {
		t.Errorf("got %q, want short", got)
	}
	if strings.Contains(TruncateRunes("short", 50), "\u2026") {
		t.Error("should not append ellipsis when under limit")
	}
}

func TestTruncateRunes_CuttingMultibyte(t *testing.T) {
	// 10 CJK characters; each is 3 bytes in UTF-8.
	in := "一二三四五六七八九十extra"
	got := TruncateRunes(in, 5)
	want := "一二三四五\u2026"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
}

func TestTruncateRunes_StripsTrailingSpaceBeforeEllipsis(t *testing.T) {
	// Cut after "first " (rune index 6). The trailing space must be stripped
	// so we don't produce "first …".
	got := TruncateRunes("first second third", 6)
	want := "first\u2026"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTruncateRunes_EmptyOrNonPositiveLimit(t *testing.T) {
	if got := TruncateRunes("", 10); got != "" {
		t.Errorf("empty input: got %q", got)
	}
	if got := TruncateRunes("hello", 0); got != "hello" {
		t.Errorf("maxRunes=0 should disable truncation: got %q", got)
	}
	if got := TruncateRunes("hello", -1); got != "hello" {
		t.Errorf("negative maxRunes should disable truncation: got %q", got)
	}
}

func TestTruncateRunes_ExactLimitNoEllipsis(t *testing.T) {
	in := "exactly10!" // 10 runes
	if got := TruncateRunes(in, 10); got != in {
		t.Errorf("10 runes with limit 10 should be unchanged; got %q", got)
	}
}

func TestTruncateRunes_EmojiBoundary(t *testing.T) {
	// Emoji are often 4 bytes in UTF-8; cutting on a byte boundary would
	// produce invalid UTF-8.
	in := "hi 🌞🌞🌞 extra"
	got := TruncateRunes(in, 5)
	if !utf8.ValidString(got) {
		t.Errorf("invalid UTF-8 after truncation: %q", got)
	}
	if !strings.HasSuffix(got, "\u2026") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}

// -- combined use as a pipeline -----------------------------------------

func TestSanitizeThenTruncate_ComposesCorrectly(t *testing.T) {
	// Input with newlines and multibyte content, cut to a small budget.
	in := "# Warden Cedar Policy System — Deep Analysis\n\nSession title: Cedar Authorization Policy Deep Dive"
	cleaned := SanitizeTitle(in)
	got := TruncateRunes(cleaned, 40)
	if !strings.HasSuffix(got, "\u2026") {
		t.Errorf("expected ellipsis, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("newlines should have been removed by SanitizeTitle, got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("invalid UTF-8: %q", got)
	}
	// Ensure the visible body has no more than 40 runes.
	body := strings.TrimSuffix(got, "\u2026")
	if utf8.RuneCountInString(body) > 40 {
		t.Errorf("body has %d runes, want <= 40: %q",
			utf8.RuneCountInString(body), body)
	}
}
