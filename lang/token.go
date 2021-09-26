package lang

//go:generate stringer -type Token
type Token int

const (
	EOF Token = iota // End of file

	// Misc
	Unexpected // anything else
	Newline    // [\t\r]
	Comment    // # ...

	LiteralInt   // 123, -1
	LiteralFloat // 0.1
	LiteralStr   // "blah\n"

	// Brackets
	OpenBrace    // {
	CloseBrace   // }
	OpenParen    // (
	CloseParen   // )
	OpenBracket  // [
	CloseBracket // ]

	// Symbols/Operators
	Comma        // ,
	Semicolon    // ;
	Colon        // :
	Assign       // =
	Plus         // +
	Dash         // -
	Star         // *
	Slash        // /
	Equal        // ==
	NotEqual     // !=
	LessThan     // <
	GreaterThan  // >
	LessEqual    // <=
	GreaterEqual // >=
	And          // &&
	Or           // ||
	Elvis        // ?:
	Rocket       // =>
	Arrow        // ->
	Not          // !
	Dot          // .
	Dollar       // $

	// Keywords
	If      // if
	Else    // else
	In      // in
	As      // as
	Class   // class
	Include // include
	Import  // import
	Func    // func

	// Type keywords
	Bool    // bool
	Str     // str
	Int     // int
	Float   // float
	Map     // map
	Struct  // struct
	Variant // variant
	// Constants
	True  // true
	False // false

	// Identifiers
	Identifier            // raw identifier, lowercase initial char
	CapitalizedIdentifier // raw identifier, uppercase initial char
	ResIdentifier         // resource identifier, Identifier with colons
)

// Precedence of Binary Operators
var Precedence = map[Token]int{
	Or:           1,
	And:          1,
	LessThan:     2,
	GreaterThan:  2,
	LessEqual:    2,
	GreaterEqual: 2,
	Equal:        2,
	NotEqual:     2,
	Plus:         3,
	Dash:         3,
	Star:         4,
	Slash:        4,
}

func (t Token) Precedence() int {
	rightPrec, ok := Precedence[t]
	if ok {
		return rightPrec
	}
	return 0
}
