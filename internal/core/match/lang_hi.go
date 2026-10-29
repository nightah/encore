package match

func hindi() *language {
	l := english()
	l.code = "hi"

	return l
}
