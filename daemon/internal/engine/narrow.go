package engine

import "github.com/cruisinme30/spelunk/daemon/internal/query"

// Narrow returns the ids that can satisfy p in ascending order, or nil for
// "any id". leaf narrows one leaf predicate, returning nil when it can't
// (paths, authors and the like). AND intersects, OR unites, and NOT never
// narrows: the ids that fail its kid are not known.
func Narrow(p query.Pred, leaf func(query.Pred) []uint32) []uint32 {
	switch p := p.(type) {
	case *query.And:
		var result []uint32
		narrowed := false
		for _, kid := range p.Kids {
			ids := Narrow(kid, leaf)
			switch {
			case ids == nil:
				continue
			case !narrowed:
				result, narrowed = ids, true
			default:
				result = Intersect(result, ids)
			}
		}
		return result
	case *query.Or:
		result := []uint32{}
		for _, kid := range p.Kids {
			ids := Narrow(kid, leaf)
			if ids == nil {
				return nil
			}
			result = Union(result, ids)
		}
		return result
	case *query.Not:
		return nil
	default:
		return leaf(p)
	}
}

// ContentLiteral is the literal every match of a text term contains, the
// one to narrow candidates with: the term itself, or what its regex requires.
func ContentLiteral(term *query.Content) string {
	if term.Literal != "" {
		return term.Literal
	}
	return query.RequiredLiteral(term.Re)
}

// Intersect returns the ids in both ascending lists. The result is never
// nil: nil means "any id" to callers, and no common id means "no id".
func Intersect(a, b []uint32) []uint32 {
	out := []uint32{}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}

// Union returns the ids in either ascending list.
func Union(a, b []uint32) []uint32 {
	out := make([]uint32, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || i < len(a) && a[i] < b[j]:
			out = append(out, a[i])
			i++
		case i == len(a) || b[j] < a[i]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}
