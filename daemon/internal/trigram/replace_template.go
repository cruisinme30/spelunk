package trigram

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// replacementReference follows regexp.Expand's documented grammar: $$,
// ${name}, or the longest $name of Unicode letters, decimal digits and
// underscores. Other dollar signs are literal, including malformed braces.
var replacementReference = regexp.MustCompile(`\$(?:\$|\{[\pL\p{Nd}_]+\}|[\pL\p{Nd}_]+)`)

// ValidateReplacement refuses undefined regex captures instead of letting
// Expand silently delete them. A synthetic match marks every declared
// group as present: Expand itself then decides numeric names, leading
// zeros and duplicate names exactly as it will during the real replace.
// An optional group that did not match real text remains valid and empty.
func ValidateReplacement(ctx context.Context, term *query.Content, replacement string) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if len(replacement) > maxReplacementPlanBytes {
		return ErrReplaceBudget
	}
	if term.Literal != "" || !strings.Contains(replacement, "$") {
		return nil
	}
	refs := newCaptureReferences(term.Re)
	return walkReplacement(replacement, func(_ string, ref string) error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if ref == "" {
			return nil
		}
		_, err := refs.indexes(ref)
		return err
	})
}

// walkReplacement visits the literal and reference pieces in expansion
// order without allocating a list proportional to the template's tokens.
func walkReplacement(template string, visit func(literal, reference string) error) error {
	for template != "" {
		loc := replacementReference.FindStringIndex(template)
		if loc == nil {
			return visit(template, "")
		}
		if err := visit(template[:loc[0]], template[loc[0]:loc[1]]); err != nil {
			return err
		}
		template = template[loc[1]:]
	}
	return nil
}

// captureReferences resolves each distinct template reference with Expand
// itself, using synthetic captures containing their own decimal indexes.
// Real capture text is never copied just to find out what a reference means.
type captureReferences struct {
	re     *regexp.Regexp
	source string
	groups []int
	cache  map[string][]int
}

func newCaptureReferences(re *regexp.Regexp) *captureReferences {
	var source strings.Builder
	groups := make([]int, 0, 2*(re.NumSubexp()+1))
	for i := range re.NumSubexp() + 1 {
		start := source.Len()
		source.WriteString(strconv.Itoa(i))
		groups = append(groups, start, source.Len())
		source.WriteByte('|')
	}
	return &captureReferences{re: re, source: source.String(), groups: groups, cache: map[string][]int{}}
}

func (r *captureReferences) indexes(ref string) ([]int, error) {
	if ref == "$$" {
		return []int{-1}, nil // the literal dollar sign
	}
	if indexes, ok := r.cache[ref]; ok {
		return indexes, nil
	}
	value := r.re.ExpandString(nil, ref, r.source, r.groups)
	index, err := strconv.Atoi(string(value))
	if err != nil {
		return nil, fmt.Errorf("undefined replacement capture %q; use ${1} or ${name} to separate a capture from following text", ref)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(ref[1:], "{"), "}")
	indexes := []int{index}
	// A canonical decimal reference is numeric, even when groups also
	// have that name. Other names choose the first matched duplicate.
	if strconv.Itoa(index) != name {
		indexes = nil
		for i, candidate := range r.re.SubexpNames() {
			if candidate == name {
				indexes = append(indexes, i)
			}
		}
	}
	r.cache[ref] = indexes
	return indexes, nil
}

// text is a reference's actual source slice, without allocating an expanded
// copy. An optional declared group that did not match contributes no text.
func (r *captureReferences) text(ref, source string, groups []int) (string, error) {
	indexes, err := r.indexes(ref)
	if err != nil {
		return "", err
	}
	for _, index := range indexes {
		if index == -1 {
			return "$", nil
		}
		if index < len(groups)/2 && groups[2*index] >= 0 {
			return source[groups[2*index]:groups[2*index+1]], nil
		}
	}
	return "", nil
}

// expandReplacement charges each literal or capture before appending it.
// Repeating a large captured group cannot first allocate a giant expansion.
func expandReplacement(ctx context.Context, refs *captureReferences, template, source string, groups []int, budget *replacementBudget) (string, error) {
	var out strings.Builder
	err := walkReplacement(template, func(literal, ref string) error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if err := budget.text(literal); err != nil {
			return err
		}
		out.WriteString(literal)
		if ref == "" {
			return nil
		}
		piece, err := refs.text(ref, source, groups)
		if err != nil {
			return err
		}
		if err := budget.text(piece); err != nil {
			return err
		}
		out.WriteString(piece)
		return nil
	})
	if err != nil {
		return "", err
	}
	return out.String(), nil
}
