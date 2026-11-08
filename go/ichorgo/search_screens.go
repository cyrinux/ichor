package ichorgo

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The screens of the active cluster in the search (U3 step 5): the apps pass their own
// catalog, titles already localised, so Go holds no screen list and no strings. Ranked with
// the scorer of the object search.

const searchScreensDefaultLimit = 20

type screenEntry struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle"`
	Keywords []string `json:"keywords"`
	Section  string   `json:"section"`
}

type screenMatch struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle"`
	Section  string  `json:"section"`
	Score    float64 `json:"score"`
}

type screenMatches struct {
	Results []screenMatch `json:"results"`
	Total   int           `json:"total"`
}

// SearchScreens ranks the screens of screensJSON ({"screens":[{id, title, subtitle, keywords,
// section}…]}) for query, best first, at most limit (20 when <= 0). The title and each of its
// words are matched as primary fields, so "kube" finds "Upgrade Kubernetes"; the subtitle,
// section and keywords as secondary ones. An empty query matches nothing.
func SearchScreens(screensJSON, query string, limit int) (out string, err error) {
	defer maskErr(&err)

	var in struct {
		Screens []screenEntry `json:"screens"`
	}
	if err := json.Unmarshal([]byte(screensJSON), &in); err != nil {
		return "", fmt.Errorf("invalid screens: %w", err)
	}

	return toJSON(searchScreens(in.Screens, query, limit))
}

func searchScreens(screens []screenEntry, query string, limit int) screenMatches {
	out := screenMatches{Results: []screenMatch{}}

	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return out
	}

	if limit <= 0 {
		limit = searchScreensDefaultLimit
	}

	for _, s := range screens {
		score, ok := screenScore(s, words)
		if !ok {
			continue
		}

		out.Results = append(out.Results, screenMatch{ID: s.ID, Title: s.Title, Subtitle: s.Subtitle, Section: s.Section, Score: score})
	}

	slices.SortStableFunc(out.Results, func(a, b screenMatch) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(len(a.Title), len(b.Title)), cmp.Compare(a.Title, b.Title))
	})

	out.Total = len(out.Results)
	if len(out.Results) > limit {
		out.Results = out.Results[:limit]
	}

	return out
}

// screenScore averages, over the query words, the best match of each word; a word that
// matches nothing drops the screen.
func screenScore(s screenEntry, words []string) (float64, bool) {
	title := strings.ToLower(s.Title)
	primary := append([]string{title}, strings.Fields(title)...)
	secondary := append([]string{s.Subtitle, s.Section}, s.Keywords...)
	total := 0.0

	for _, word := range words {
		best := 0.0
		for _, field := range primary {
			best = max(best, searchMatch(field, word))
		}

		for _, field := range secondary {
			best = max(best, searchSecondary*searchMatch(strings.ToLower(field), word))
		}

		if best == 0 {
			return 0, false
		}

		total += best
	}

	return total / float64(len(words)), true
}
