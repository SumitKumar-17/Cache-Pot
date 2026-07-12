package memory

import "strings"

// Kind categorizes a Memory by how long it should persist / how it's used.
type Kind int

const (
	ShortTerm Kind = iota
	LongTerm
	Episodic
	Semantic
)

func (k Kind) String() string {
	switch k {
	case ShortTerm:
		return "short_term"
	case LongTerm:
		return "long_term"
	case Episodic:
		return "episodic"
	case Semantic:
		return "semantic"
	default:
		return "unknown"
	}
}

// ParseKind parses s (case-insensitive) into a Kind, reporting ok=false if s
// doesn't match one of "short_term", "long_term", "episodic", "semantic".
func ParseKind(s string) (Kind, bool) {
	switch strings.ToLower(s) {
	case "short_term":
		return ShortTerm, true
	case "long_term":
		return LongTerm, true
	case "episodic":
		return Episodic, true
	case "semantic":
		return Semantic, true
	default:
		return 0, false
	}
}
