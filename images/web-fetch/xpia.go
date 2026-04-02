package main

import (
	"regexp"
	"strings"
)

type xpiaPattern struct {
	re   *regexp.Regexp
	desc string
}

var xpiaPatterns = func() []xpiaPattern {
	raw := []struct {
		pattern string
		desc    string
	}{
		{`(?:^|\n)\s*(?:system|assistant)\s*:`, "role impersonation"},
		{`ignore\s+(?:previous|above|all)\s+instructions`, "instruction override"},
		{`disregard\s+(?:previous|above|your)\s+(?:instructions|constraints|rules)`, "instruction override"},
		{`you\s+are\s+now\s+(?:a|an|the)\b`, "identity override"},
		{`new\s+instructions?\s*:`, "instruction injection"},
		{`<\s*(?:system|prompt|instruction)\s*>`, "tag injection"},
		{`forget\s+(?:everything|all|your)\s+(?:previous|prior)`, "memory wipe"},
		{`act\s+as\s+if\s+you\s+(?:have\s+no|don't\s+have)\s+constraints`, "constraint bypass"},
		{`(?:do\s+not|don't)\s+follow\s+(?:your|the|any)\s+(?:rules|constraints|instructions)`, "constraint bypass"},
	}
	out := make([]xpiaPattern, 0, len(raw))
	for _, r := range raw {
		out = append(out, xpiaPattern{
			re:   regexp.MustCompile("(?i)" + r.pattern),
			desc: r.desc,
		})
	}
	return out
}()

func XPIAScan(text string) []string {
	if len(text) < 10 {
		return nil
	}
	lower := strings.ToLower(text)
	var flags []string
	seen := make(map[string]bool)
	for _, p := range xpiaPatterns {
		if p.re.MatchString(lower) && !seen[p.desc] {
			flags = append(flags, p.desc)
			seen[p.desc] = true
		}
	}
	return flags
}
