package loanword

// maxForms is the most forms Native returns for one title.
const maxForms = 4

// Phones english gives that the writers treat specially.
const (
	schwa = "@"
	// postR is an r after a vowel ("car", "bird").
	postR = "R"
	// geminate marks a doubled consonant after a short vowel ("Harry Potter", "Rocky", "mission"), which Japanese writes with ッ and the other languages leave out.
	geminate = "Q"
)
