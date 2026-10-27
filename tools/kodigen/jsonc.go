package main

// stripComments removes // line comments and /* */ block comments from JSON-with-comments input, leaving string literals untouched. Removed comment bytes are replaced by spaces (newlines are kept) so that decoder error offsets still point at the right line.
func stripComments(src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)

	const (
		code = iota
		str
		lineComment
		blockComment
	)

	state := code
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch state {
		case code:
			switch {
			case c == '"':
				state = str
			case c == '/' && i+1 < len(out) && out[i+1] == '/':
				state = lineComment
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '/' && i+1 < len(out) && out[i+1] == '*':
				state = blockComment
				out[i], out[i+1] = ' ', ' '
				i++
			}
		case str:
			switch c {
			case '\\':
				i++ // skip the escaped byte
			case '"':
				state = code
			}
		case lineComment:
			if c == '\n' {
				state = code
			} else {
				out[i] = ' '
			}
		case blockComment:
			if c == '*' && i+1 < len(out) && out[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				i++
				state = code
			} else if c != '\n' {
				out[i] = ' '
			}
		}
	}

	return out
}
