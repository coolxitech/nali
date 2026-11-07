package entity

import (
	"net/netip"
	"sort"

	"github.com/coolxitech/nali/internal/db"
	"github.com/coolxitech/nali/pkg/dbif"
	"github.com/coolxitech/nali/pkg/re"
)

// LookupFunc resolves one token against the databases of typ.
type LookupFunc func(typ dbif.QueryType, text string) ([]*db.Result, error)

// ParseLine parse a line into entities
func ParseLine(line string) Entities {
	// db.Find is the fatal lookup: like before the non-fatal split it
	// terminates the process when the selected databases cannot be
	// initialised. Callers that must survive such failures use ParseLineWith
	// with db.FindOrError.
	es, _ := ParseLineWith(line, func(typ dbif.QueryType, text string) ([]*db.Result, error) {
		return db.Find(typ, text), nil
	})
	return es
}

// ParseLineWith parses a line into entities, resolving each token with lookup.
// A token whose lookup fails keeps its entity type but gets no results; the
// first such error is returned while the remaining tokens are still parsed.
func ParseLineWith(line string, lookup LookupFunc) (Entities, error) {
	ip4sLoc := re.IPv4Re.FindAllStringIndex(line, -1)
	ip6sLoc := re.IPv6Re.FindAllStringIndex(line, -1)
	domainsLoc := re.DomainRe.FindAllStringIndex(line, -1)

	tmp := make(Entities, 0, len(ip4sLoc)+len(ip6sLoc)+len(domainsLoc))
	for _, e := range ip4sLoc {
		tmp = append(tmp, &Entity{
			Loc:  *(*[2]int)(e),
			Type: TypeIPv4,
			Text: line[e[0]:e[1]],
		})
	}
	for _, e := range ip6sLoc {
		text := line[e[0]:e[1]]
		if ip, _ := netip.ParseAddr(text); !ip.Is4In6() {
			tmp = append(tmp, &Entity{
				Loc:  *(*[2]int)(e),
				Type: TypeIPv6,
				Text: text,
			})
		}
	}
	for _, e := range domainsLoc {
		tmp = append(tmp, &Entity{
			Loc:  *(*[2]int)(e),
			Type: TypeDomain,
			Text: line[e[0]:e[1]],
		})
	}

	sort.Sort(tmp)
	var es Entities
	var firstErr error

	idx := 0
	for _, e := range tmp {
		start := e.Loc[0]
		if start >= idx {
			if start > idx {
				es = append(es, &Entity{
					Loc:  [2]int{idx, start},
					Type: TypePlain,
					Text: line[idx:start],
				})
			}
			results, err := lookup(dbif.QueryType(e.Type), e.Text)
			if err != nil {
				// keep the entity type: the token is still an address, it just
				// has no answer because the databases could not be resolved
				if firstErr == nil {
					firstErr = err
				}
			} else if !e.setResults(results) {
				e.Type = TypePlain
			}
			idx = e.Loc[1]
			es = append(es, e)
		}
	}
	if total := len(line); idx < total {
		es = append(es, &Entity{
			Loc:  [2]int{idx, total},
			Type: TypePlain,
			Text: line[idx:total],
		})
	}
	return es, firstErr
}

// setResults stores the lookup results of one entity and reports whether any
// database answered. The top-level Source/InfoText/Info fields hold the
// first answer with a non-empty text (or else the first answer), and when
// several databases are selected Results lists all of them in selection
// order, with empty text and null info for databases without an answer.
func (e *Entity) setResults(results []*db.Result) bool {
	var primary *db.Result
	for _, r := range results {
		if !r.Found() {
			continue
		}
		if primary == nil || (primary.Text() == "" && r.Text() != "") {
			primary = r
		}
	}
	if primary == nil {
		return false
	}

	e.InfoText = primary.Text()
	e.Info = primary.Result
	e.Source = primary.Source
	if len(results) > 1 {
		for _, r := range results {
			entry := Result{Source: r.Source, Text: r.Text()}
			if r.Found() {
				entry.Info = r.Result
			}
			e.Results = append(e.Results, entry)
		}
	}
	return true
}
