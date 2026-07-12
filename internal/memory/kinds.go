package memory

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
