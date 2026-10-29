package match

func japanese() *language {
	l := english()
	l.code = "ja"

	return l
}
