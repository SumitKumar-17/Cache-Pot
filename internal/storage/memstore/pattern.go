package memstore

// globMatch implements Redis-style glob matching (as used by KEYS/SCAN
// MATCH): '*' matches any sequence, '?' matches any single character, and
// '[...]' matches a character class (with optional leading '^' negation and
// 'a-z' ranges). It intentionally does not support '\\' escaping beyond what
// is needed for KEYS/SCAN command coverage.
func globMatch(pattern, s string) bool {
	return globMatchBytes([]byte(pattern), []byte(s))
}

func globMatchBytes(pattern, s []byte) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			for len(pattern) > 1 && pattern[1] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globMatchBytes(pattern[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			s = s[1:]
			pattern = pattern[1:]
		case '[':
			if len(s) == 0 {
				return false
			}
			end := indexByte(pattern, ']')
			if end < 0 {
				// malformed class, treat '[' literally
				if s[0] != '[' {
					return false
				}
				s = s[1:]
				pattern = pattern[1:]
				continue
			}
			class := pattern[1:end]
			neg := false
			if len(class) > 0 && class[0] == '^' {
				neg = true
				class = class[1:]
			}
			matched := matchClass(class, s[0])
			if matched == neg {
				return false
			}
			s = s[1:]
			pattern = pattern[end+1:]
		case '\\':
			if len(pattern) > 1 {
				if len(s) == 0 || s[0] != pattern[1] {
					return false
				}
				s = s[1:]
				pattern = pattern[2:]
			} else {
				if len(s) == 0 || s[0] != '\\' {
					return false
				}
				s = s[1:]
				pattern = pattern[1:]
			}
		default:
			if len(s) == 0 || s[0] != pattern[0] {
				return false
			}
			s = s[1:]
			pattern = pattern[1:]
		}
	}
	return len(s) == 0
}

func indexByte(b []byte, c byte) int {
	for i, ch := range b {
		if ch == c {
			return i
		}
	}
	return -1
}

func matchClass(class []byte, c byte) bool {
	for i := 0; i < len(class); i++ {
		if i+2 < len(class) && class[i+1] == '-' {
			if class[i] <= c && c <= class[i+2] {
				return true
			}
			i += 2
			continue
		}
		if class[i] == c {
			return true
		}
	}
	return false
}
