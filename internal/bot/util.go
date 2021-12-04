package bot

import (
	"html"
	"regexp"
	"strings"

	strip "github.com/grokify/html-strip-tags-go"
)

func findAndDelete(s []string, item string) []string {
	idx := 0
	for _, i := range s {
		if i != item {
			s[idx] = i
			idx++
		}
	}
	return s[:idx]
}

func trimDescription(desc string, limit int) string {
	if limit == 0 {
		return ""
	}
	desc = strings.Trim(
		strip.StripTags(
			regexp.MustCompile("\n+").ReplaceAllLiteralString(
				strings.ReplaceAll(
					regexp.MustCompile(`<br(| /)>`).ReplaceAllString(
						html.UnescapeString(desc), "<br>"),
					"<br>", "\n"),
				"\n")),
		"\n")

	contentDescRune := []rune(desc)
	if len(contentDescRune) > limit {
		desc = string(contentDescRune[:limit])
	}

	return desc
}
