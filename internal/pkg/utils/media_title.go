package utils

import (
	"regexp"
	"strconv"
	"strings"
)

// NormalizeMediaTitleAndSeason moves clear terminal season markers out of a
// series title while preserving the season number for media-library paths.
func NormalizeMediaTitleAndSeason(title string, season int) (string, int) {
	if title == "" {
		if season <= 0 {
			return title, 1
		}
		return title, season
	}

	cleaned := title
	detectedSeason := season
	matched := false

	patterns := []string{
		`[\s　]*[\(（\[]?[\s　]*第[\s　]*([0-9０-９一二三四五六七八九十两]+)[\s　]*季[\s　]*[\)）\]]?[\s　]*$`,
		`[\s　]*[\(（\[]?[\s　]*第[\s　]*([0-9０-９一二三四五六七八九十两]+)[\s　]*期[\s　]*[\)）\]]?[\s　]*$`,
		`[\s　]*[\(（\[]?[\s　]*第[\s　]*([0-9０-９一二三四五六七八九十两]+)[\s　]*シリーズ[\s　]*[\)）\]]?[\s　]*$`,
		`[\s　]*[\(（\[]?[\s　]*[sSｓＳ][eEｅＥ][aAａＡ][sSｓＳ][oOｏＯ][nNｎＮ][\s　]*([0-9０-９IVXivx]+)[\s　]*[\)）\]]?[\s　]*$`,
		`(?:[\s　]|^)[\(（\[]?[\s　]*[sSｓＳ][\s　]*([0-9０-９]{1,2})[\s　]*[\)）\]]?[\s　]*$`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(title); len(matches) > 1 {
			if parsed := parseMediaSeasonToken(matches[1]); parsed > 0 {
				detectedSeason = parsed
			}
			cleaned = re.ReplaceAllString(title, "")
			matched = true
			break
		}
	}
	if !matched {
		if detectedSeason <= 0 {
			detectedSeason = 1
		}
		return title, detectedSeason
	}

	cleaned = strings.TrimSpace(strings.Trim(cleaned, "-_·:："))
	if cleaned == "" {
		cleaned = title
	}
	if detectedSeason <= 0 {
		detectedSeason = 1
	}

	return cleaned, detectedSeason
}

// MediaLibraryTitle returns the title component used for ${title} and the
// top-level media-library folder. Subscription.Name may still be a UI label in
// older records, so path generation cleans it defensively.
func MediaLibraryTitle(title string) string {
	cleaned, _ := NormalizeMediaTitleAndSeason(title, 1)
	return cleaned
}

func parseMediaSeasonToken(token string) int {
	token = strings.TrimSpace(foldFullwidth(token))
	if token == "" {
		return 0
	}

	if n, err := strconv.Atoi(token); err == nil {
		return n
	}
	if n := romanToInt(strings.ToUpper(token)); n > 0 {
		return n
	}
	if n, ok := chineseNumeralToInt(token); ok && n > 0 {
		return n
	}

	return 0
}

func romanToInt(s string) int {
	if s == "" {
		return 0
	}

	vals := map[rune]int{'I': 1, 'V': 5, 'X': 10}
	total := 0
	prev := 0

	for i := len(s) - 1; i >= 0; i-- {
		v, ok := vals[rune(s[i])]
		if !ok {
			return 0
		}
		if v < prev {
			total -= v
		} else {
			total += v
		}
		prev = v
	}

	return total
}
