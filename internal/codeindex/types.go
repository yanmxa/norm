package codeindex

// Symbol is a statically declared callable. Calls are conservative lexical references.
type Symbol struct {
	ID, Name, Qualified, Kind, File, Language, Signature, Fingerprint string
	Unresolved                                                        int // Dynamic, receiver, or shadowed call sites not emitted in Calls.
	StartLine, EndLine                                                int
	Exported                                                          bool
	Calls                                                             []Call
}
type Call struct {
	Name, Qualifier string
	Line            int
}
type Import struct {
	Alias, Path string
	Name        string
}
type File struct {
	Path, Language, Package string
	Symbols                 []Symbol
	Imports                 []Import
	Identifiers             []string
}
