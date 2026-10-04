package history

import (
	"strings"
	"time"
	"unicode"
)

// AuthorCount is one author of a repo with how much they committed.
type AuthorCount struct {
	Name    string // after .mailmap
	Emails  []string
	Commits int
	LastAt  time.Time
}

// Authors counts the commits of each author in the store, by name.
func (s *Store) Authors() []AuthorCount {
	byName := map[string]*AuthorCount{}
	var order []*AuthorCount
	for _, seg := range s.segments {
		for i := range seg.commits {
			c := &seg.commits[i]
			author, ok := byName[c.AuthorName]
			if !ok {
				author = &AuthorCount{Name: c.AuthorName, LastAt: c.At}
				byName[c.AuthorName] = author
				order = append(order, author)
			}
			author.Commits++
			if !containsFold(author.Emails, c.AuthorEmail) {
				author.Emails = append(author.Emails, c.AuthorEmail)
			}
			if c.At.After(author.LastAt) {
				author.LastAt = c.At
			}
		}
	}
	authors := make([]AuthorCount, len(order))
	for i, author := range order {
		authors[i] = *author
	}
	return authors
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// WordCount says how many commit subjects use a word or phrase, and when
// one last did.
type WordCount struct {
	Commits int
	LastAt  time.Time
}

// minWordLength keeps "a", "to" and the like out of message suggestions.
const minWordLength = 3

// stopWords are common words that say nothing about a change.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true, "into": true, "when": true,
	"that": true, "this": true, "not": true, "are": true, "was": true, "use": true,
}

// Words counts the words, and the two-word phrases, of the subjects of
// commits made after after: "retry", "fix flaky". Words are lowercase.
func (s *Store) Words(after time.Time) map[string]WordCount {
	counts := map[string]WordCount{}
	for _, seg := range s.segments {
		for i := range seg.commits {
			c := &seg.commits[i]
			if c.At.Before(after) {
				continue
			}
			for term := range subjectTerms(c.Subject) {
				count := counts[term]
				count.Commits++
				if c.At.After(count.LastAt) {
					count.LastAt = c.At
				}
				counts[term] = count
			}
		}
	}
	return counts
}

// subjectTerms returns a subject's words and adjacent-word phrases, once each.
func subjectTerms(subject string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(subject), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	})
	terms := map[string]bool{}
	for i, word := range words {
		if len(word) < minWordLength || stopWords[word] {
			continue
		}
		terms[word] = true
		if i+1 < len(words) && len(words[i+1]) >= minWordLength && !stopWords[words[i+1]] {
			terms[word+" "+words[i+1]] = true
		}
	}
	return terms
}
