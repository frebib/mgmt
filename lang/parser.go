package lang

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/purpleidea/mgmt/lang/interfaces"
	"github.com/purpleidea/mgmt/lang/types"
	"github.com/purpleidea/mgmt/util"
)

type Flag int

const (
	KeepComments Flag = 1 << iota
	SkipInterpolate
)

type Parser struct {
	lexer *Lexer
	flags Flag

	errors chan error

	prev struct {
		pos  interfaces.Pos
		tok  Token
		text string
	}
	next Token
	text string

	// skippedNewline is true if the last advance() skipped over newline tokens
	skippedNewline bool
}

func LexParse(input io.Reader, flags Flag) (interfaces.Stmt, error) {
	l := NewLexer(input)
	p := NewParser(l, flags)
	return p.Parse()
}

func NewParser(lexer *Lexer, flags Flag) Parser {
	return Parser{
		lexer: lexer,
		flags: flags,
	}
}

func (p *Parser) Parse() (prog *StmtProg, err error) {
	// TODO(frebib): Clean up parser error handling
	defer func() {
		if thing := recover(); thing != nil {
			if recovered, ok := thing.(error); ok {
				prog = nil
				err = recovered
				return
			}
			if str, ok := thing.(string); ok && str == "too many errors" {
				prog = nil
				return
			}
			panic(thing)
		}
	}()
	defer func() {
		for {
			select {
			case e := <-p.errors:
				if err == nil {
					err = e
				}
			default:
				return
			}
		}
	}()

	p.errors = make(chan error, 2)
	// Start at the top
	p.advance()

	body := p.parseBody()
	if p.next != EOF {
		p.errorUnexpected()
	}

	return body, nil
}

func (p *Parser) pos() interfaces.Pos {
	return interfaces.Pos{
		Line:   p.lexer.Line() + 1,
		Column: p.lexer.Column() + 1,
		Length: utf8.RuneCountInString(p.text),
	}
}

func (p *Parser) lookahead() {
	p.next = p.lexer.Lex(nil)
	if p.next != EOF {
		p.text = p.lexer.Text()
	}
}

func (p *Parser) advance() {
	// Reset
	p.skippedNewline = false

	// Store previously accepted token position
	p.prev.tok = p.next
	p.prev.text = p.text
	p.prev.pos = p.pos()

	//fmt.Printf("consume\t%+v: %s\n", p.next, p.text)
	p.lookahead()

	for {
		switch p.next {
		case Unexpected:
			p.errorUnexpected()
		// Chomp newlines
		case Newline:
			p.skippedNewline = true
			p.lookahead()
		case Comment:
			p.skippedNewline = true // comments swallow trailing newlines
			comment := p.parseComment()
			if p.flags&KeepComments > 0 {
				fmt.Printf("comment %#v\n", comment.Value)
			}
		default:
			return
		}
	}
}

func (p *Parser) accept(token Token) bool {
	if p.next == token {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) expect(token ...Token) {
	for i := range token {
		if p.next != token[i] {
			p.error(&UnexpectedToken{
				ParseError: ParseError{Pos: p.pos()},
				Token:      p.next,
				Text:       p.text,
			})
			return
		}
		p.advance()
	}
}

func (p *Parser) error(err error) {
	panic(err)
	select {
	case p.errors <- err:
		return
	default:
		panic("too many errors")
	}
}

func (p *Parser) syntaxError(pos interfaces.Pos, format string, args ...interface{}) {
	p.error(&SyntaxError{
		ParseError: ParseError{pos},
		Message:    fmt.Sprintf(format, args...),
	})
}

func (p *Parser) errorUnexpected() {
	p.error(&UnexpectedToken{
		ParseError: ParseError{Pos: p.pos()},
		Token:      p.next,
		Text:       p.text,
	})
}

func (p *Parser) expectSeparator(separator, close Token, context string) {
	// Always accept separator, even if it immediately precedes the close token
	if p.accept(separator) {
		return
	}

	if !p.skippedNewline && p.next == close {
		return
	}

	// Newlines that aren't preceded by the separator are disallowed
	if p.skippedNewline {
		context = "before newline in " + context
	} else {
		context = fmt.Sprintf("before %v in %s", p.next, context)
	}

	// FIXME(frebib): Tabs account for 1 column so syntax errors appear misaligned
	pos := p.prev.pos
	pos.Column += utf8.RuneCountInString(p.prev.text)
	pos.Length = 1

	p.error(&ExpectedSeparator{
		ParseError: ParseError{pos},
		Separator:  separator,
		Context:    context,
	})
}

func (p *Parser) parseComment() *StmtComment {
	var comments []string
	//var pos = p.pos()

	for p.next == Comment {
		s := p.text
		// Trim leading #, and leading/trailing whitespace
		s = strings.TrimSpace(s[1:len(s)])
		comments = append(comments, s)

		p.lookahead()

		// Consume single newline at the end of comment lines so
		// multi-line comments are consumed together into a single block
		if p.next == Newline {
			p.lookahead()
		}
	}
	if len(comments) == 0 {
		return nil
	}

	comment := strings.Join(comments, "\n")
	//return &StmtComment{Value: comment, Pos: pos}, nil
	return &StmtComment{Value: comment}
}

func (p *Parser) parseBody() *StmtProg {
	prog := StmtProg{
		Prog: []interfaces.Stmt{},
	}

	for p.next != EOF {
		stmt := p.parseStmt()
		if stmt == nil {
			break
		}
		prog.Prog = append(prog.Prog, stmt)
	}

	return &prog
}

func (p *Parser) parseStmt() interfaces.Stmt {
	statements := []func() interfaces.Stmt{
		p.parseStmtRes,
		p.parseStmtEdge,
		p.parseStmtFunc,
		p.parseStmtBind,
		p.parseStmtIf,
		p.parseStmtImport,
		p.parseStmtInclude,
		p.parseStmtClass,
	}

	for _, fn := range statements {
		stmt := fn()
		if stmt != nil {
			return stmt
		}
	}

	return nil
}

func (p *Parser) parseEdgeHalf() *StmtEdgeHalf {
	if !p.accept(CapitalizedIdentifier) {
		return nil
	}
	pos := p.prev.pos
	kind := p.prev.text
	p.expect(OpenBracket)
	expr := p.parseExpr()
	p.expect(CloseBracket)
	pos.Length = p.prev.pos.Column - pos.Column + 1

	return &StmtEdgeHalf{
		Name: expr,
		Kind: strings.ToLower(kind),
		pos:  pos,
	}
}

func (p *Parser) parseStmtEdge() interfaces.Stmt {
	half := p.parseEdgeHalf()
	if half == nil {
		return nil
	}
	halves := []*StmtEdgeHalf{half}

	// SendRecv is: <edge-half>.<prop> -> <edge>.<prop>
	if p.accept(Dot) {
		p.expect(Identifier)
		half.SendRecv = p.prev.text

		p.expect(Arrow)

		right := p.parseEdgeHalf()
		if right == nil {
			p.errorUnexpected()
		}
		p.expect(Dot, Identifier)
		right.SendRecv = p.prev.text

		halves = append(halves, right)

	} else {
		// <edge-half> -> <edge-half> [-> <edge-half> ...]
		for p.accept(Arrow) {
			half = p.parseEdgeHalf()
			if half == nil {
				p.errorUnexpected()
			}
			halves = append(halves, half)
		}

		if len(halves) < 2 {
			p.syntaxError(half.Pos(), "edges cannot be used alone")
		}
	}

	return &StmtEdge{
		EdgeHalfList: halves,
		//Notify: false, // unused here
	}
}

func (p *Parser) parseStmtRes() interfaces.Stmt {
	kind := p.text
	if !(p.accept(Identifier) || p.accept(ResIdentifier)) {
		return nil
	}
	name := p.parseExpr()
	p.expect(OpenBrace)

	contents := []StmtResContents{}
	for {
		field := p.parseStmtResContents()
		if field == nil {
			break
		}
		contents = append(contents, field)
		p.expectSeparator(Comma, CloseBrace, "resource")
	}
	p.expect(CloseBrace)

	return &StmtRes{
		Kind:     kind,
		Name:     name,
		Contents: contents,
	}
}

func (p *Parser) parseStmtFunc() interfaces.Stmt {
	if !p.accept(Func) {
		return nil
	}
	name := p.text
	p.expect(Identifier)
	args := p.parseArgDefinitions("func")
	if args == nil {
		p.errorUnexpected()
	}
	// Optional return type hint
	typ := p.parseTypeHint()
	p.expect(OpenBrace)
	body := p.parseExpr()
	p.expect(CloseBrace)

	if typ != nil {
		isFullyTyped := true // true if set
		m := make(map[string]*types.Type, len(args))
		ord := make([]string, len(args))

		for i, a := range args {
			if a.Type == nil {
				// at least one is unknown, can't run SetType...
				isFullyTyped = false
				break
			}
			m[a.Name] = a.Type
			ord[i] = a.Name
		}
		if isFullyTyped {
			typ := &types.Type{
				Kind: types.KindFunc,
				Map:  m,
				Ord:  ord,
				Out:  typ,
			}
			if err := body.SetType(typ); err != nil {
				// this will ultimately cause a parser error to occur...
				p.error(err)
				return nil
			}
		}
	}

	return &StmtFunc{
		Name: name,
		Func: &ExprFunc{
			Args:   args,
			Return: typ, // can be nil
			Body:   body,
		},
	}
}

func (p *Parser) parseArgDefinitions(context string) []*interfaces.Arg {
	if !p.accept(OpenParen) {
		return nil
	}
	context = fmt.Sprintf("%s arguments", context)

	args := []*interfaces.Arg{}
	for {
		if !p.accept(Dollar) {
			break
		}
		name := p.text
		p.expect(Identifier)
		typ := p.parseTypeHint()

		arg := &interfaces.Arg{
			Name: name,
			Type: typ, // can be nil
		}
		args = append(args, arg)

		p.expectSeparator(Comma, CloseParen, context)
	}
	p.expect(CloseParen)

	return args
}

func (p *Parser) parseStmtResContents() StmtResContents {
	ident := p.text

	switch {
	case p.accept(Identifier):
		var value, condition interfaces.Expr

		p.expect(Rocket)
		value = p.parseExpr()
		if p.accept(Elvis) {
			// Condition is the first expr when using elvis:
			// <ident> => <condition> ?: <value>
			condition = value
			value = p.parseExpr()
		}

		return &StmtResField{
			Field:     ident,
			Value:     value,
			Condition: condition,
		}

	// Meta => ...
	case strings.ToLower(ident) == strings.ToLower(MetaField) &&
		p.accept(CapitalizedIdentifier):
		var value, condition interfaces.Expr

		// Use 'Meta' as property name, or Meta:<key> if <key> is provided
		prop := ident
		if p.accept(Colon) {
			prop = p.text
			p.expect(Identifier)
		}

		p.expect(Rocket)
		value = p.parseExpr()
		if p.accept(Elvis) {
			condition = value
			value = p.parseExpr()
		}

		return &StmtResMeta{
			Property:  prop,
			MetaExpr:  value,
			Condition: condition,
		}

	case p.accept(CapitalizedIdentifier):
		p.expect(Rocket)

		// Simple and most common case
		// <cap-ident> => <cap-ident>
		edge := p.parseEdgeHalf()
		if edge != nil {
			return &StmtResEdge{
				Property: ident,
				EdgeHalf: edge,
			}
		}

		// No capitalised identifier here so assume <expr> ?: <edge-half>
		condition := p.parseExpr()
		p.expect(Elvis)
		edge = p.parseEdgeHalf()

		return &StmtResEdge{
			Property:  ident,
			EdgeHalf:  edge,
			Condition: condition,
		}
	}

	return nil
}

// $abc = true
// $abc bool = true
func (p *Parser) parseStmtBind() interfaces.Stmt {
	if !p.accept(Dollar) {
		return nil
	}
	ident := p.text
	p.expect(Identifier)
	// Optional type hint
	typ := p.parseTypeHint()
	p.expect(Assign)
	value := p.parseExpr()

	if typ != nil {
		err := value.SetType(typ)
		if err != nil {
			p.error(err)
			return nil
		}
	}

	return &StmtBind{Ident: ident, Value: value}
}

func (p *Parser) parseStmtIf() interfaces.Stmt {
	if !p.accept(If) {
		return nil
	}
	condition := p.parseExpr()
	p.expect(OpenBrace)
	bodyIf := p.parseBody()
	p.expect(CloseBrace)

	// Optional else block in if statements
	var bodyElse interfaces.Stmt
	if p.next == Else {
		p.advance()
		p.expect(OpenBrace)
		bodyElse = p.parseBody()
		p.expect(CloseBrace)
	}

	return &StmtIf{
		condition,
		bodyIf,
		bodyElse,
	}
}

func (p *Parser) parseStmtImport() interfaces.Stmt {
	if !p.accept(Import) {
		return nil
	}
	name := p.text[1 : len(p.text)-1]
	p.expect(LiteralStr)

	var alias string
	if p.accept(As) {
		alias = p.text
		// import "foo" as whatever
		// import "foo" as *
		if !p.accept(Star) {
			p.expect(Identifier)
		}
	}

	return &StmtImport{
		Name:  name,
		Alias: alias,
	}
}

func (p *Parser) parseStmtClass() interfaces.Stmt {
	if !p.accept(Class) {
		return nil
	}
	name := p.text
	p.expect(Identifier)
	args := p.parseArgDefinitions("class")
	p.expect(OpenBrace)
	body := p.parseBody()
	p.expect(CloseBrace)

	return &StmtClass{
		Name: name,
		Args: args,
		Body: body,
	}
}

func (p *Parser) parseStmtInclude() interfaces.Stmt {
	if !p.accept(Include) {
		return nil
	}
	name := p.parseDottedIdentifier()
	args := p.parseExprParenList()

	return &StmtInclude{
		Name: name,
		Args: args,
	}
}

func (p *Parser) parseExpr() interfaces.Expr {
	return p.parseExprBinary(0)
}

func (p *Parser) parseExprBinary(precedence int) interfaces.Expr {
	left := p.parseExprValue()

	for {
		rightPrec := p.next.Precedence()
		if precedence >= rightPrec {
			return left
		}

		op := p.text
		p.advance()
		right := p.parseExprBinary(rightPrec)

		left = &ExprCall{
			Name: operatorFuncName,
			Args: []interfaces.Expr{
				// TODO(frebib): Store the operator token instead of text
				&ExprStr{V: op}, // operator first
				left,
				right,
			},
		}
	}
}
func (p *Parser) parseExprValue() interfaces.Expr {
	value := p.text

	switch {
	case p.accept(Not):
		// Recurse. Calling parseExpr() here gives the wrong precedence.
		expr := p.parseExprValue()
		return &ExprCall{
			Name: operatorFuncName,
			Args: []interfaces.Expr{
				// TODO(frebib): Use token for binary/unary operators
				&ExprStr{V: value},
				expr,
			},
		}

	case p.accept(OpenParen):
		expr := p.parseExpr()
		p.expect(CloseParen)
		return expr

	case p.accept(True):
		return &ExprBool{V: true}
	case p.accept(False):
		return &ExprBool{V: false}

	case p.accept(LiteralInt):
		// TODO(frebib): Implement int/float overflow checks
		i, _ := strconv.ParseInt(value, 10, 64)
		return &ExprInt{V: i}

	case p.accept(LiteralFloat):
		f, _ := strconv.ParseFloat(value, 64)
		return &ExprFloat{V: f}

	case p.accept(LiteralStr):
		// Strip leading & trailing double quotes
		text := value[1 : len(value)-1]

		if p.flags&SkipInterpolate > 0 {
			return &ExprStr{V: text}
		}

		expr, err := InterpolateStr(text, p.pos())
		if err != nil {
			p.error(err)
			return nil
		}
		return expr

	case p.accept(Dollar):
		name := p.parseDottedIdentifier()

		// calling a function that's stored in a variable (a lambda)
		// `$foo(4, "hey")` # call function value
		if p.next == OpenParen {
			args := p.parseExprParenList()
			return &ExprCall{
				Name: name,
				Args: args,
				// Instead of `Var: true`, we could have added a `$`
				// prefix to the Name, but I felt this was more elegant.
				Var: true,
			}
		}

		return &ExprVar{Name: name}

	case p.accept(If):
		cond := p.parseExpr()
		p.expect(OpenBrace)
		then := p.parseExpr()
		p.expect(CloseBrace, Else, OpenBrace)
		els := p.parseExpr()
		p.expect(CloseBrace)
		return &ExprIf{
			Condition:  cond,
			ThenBranch: then,
			ElseBranch: els,
		}

	// [<expr>, ...]
	case p.accept(OpenBracket):
		elems := []interfaces.Expr{}
		for {
			expr := p.parseExpr()
			if expr == nil {
				break
			}
			elems = append(elems, expr)
			p.expectSeparator(Comma, CloseBracket, "list")
		}
		p.expect(CloseBracket)

		return &ExprList{
			Elements: elems,
		}

	// {<ident>: <expr>, ...}
	case p.accept(OpenBrace):
		elems := []*ExprMapKV{}
		for {
			key := p.parseExpr()
			if key == nil {
				break
			}
			p.expect(Rocket)
			val := p.parseExpr()
			if val == nil {
				p.errorUnexpected()
				return nil
			}
			p.expectSeparator(Comma, CloseBrace, "map")

			elems = append(elems, &ExprMapKV{key, val})
		}
		p.expect(CloseBrace)

		return &ExprMap{KVs: elems}

	// struct{<ident>: <expr>, ...}
	case p.accept(Struct):
		p.expect(OpenBrace)
		fields := []*ExprStructField{}
		for {
			ident := p.text
			if !p.accept(Identifier) {
				break
			}
			p.expect(Rocket)
			elem := p.parseExpr()
			if elem == nil {
				break
			}
			p.expectSeparator(Comma, CloseBrace, "struct")

			fields = append(fields, &ExprStructField{Name: ident, Value: elem})
		}
		p.expect(CloseBrace)

		return &ExprStruct{Fields: fields}

	case p.accept(Func):
		args := p.parseArgDefinitions("func")
		if args == nil {
			p.errorUnexpected()
		}
		typ := p.parseTypeHint() // optional, might be nil
		p.expect(OpenBrace)
		body := p.parseExpr()
		p.expect(CloseBrace)

		fn := &ExprFunc{
			Args:   args,
			Return: typ,
			Body:   body,
		}

		isFullyTyped := typ != nil // true if set
		m := make(map[string]*types.Type, len(args))
		ord := make([]string, len(args))
		for i, a := range args {
			if a.Type == nil {
				// at least one is unknown, can't run SetType...
				isFullyTyped = false
				break
			}
			m[a.Name] = a.Type
			ord[i] = a.Name
		}
		if isFullyTyped {
			typ := &types.Type{
				Kind: types.KindFunc,
				Map:  m,
				Ord:  ord,
				Out:  typ,
			}
			if err := fn.SetType(typ); err != nil {
				// this will ultimately cause a parser error to occur...
				p.error(err)
				return nil
			}
		}
		return fn

	// foo, foo.bar.baz
	case p.next == Identifier:
		name := p.parseDottedIdentifier()
		args := p.parseExprParenList()
		if args == nil {
			p.errorUnexpected()
		}

		return &ExprCall{
			Name: name,
			Args: args,
		}

	case p.next == CapitalizedIdentifier:
		halves := p.parseStmtEdge()
		if halves != nil {
		}
	}

	return nil
}

func (p *Parser) parseDottedIdentifier() string {
	name := p.text
	p.expect(Identifier)
	for p.accept(Dot) {
		name += (".") + p.text
		p.expect(Identifier)
	}
	return name
}

func (p *Parser) parseExprParenList() []interfaces.Expr {
	if !p.accept(OpenParen) {
		return nil
	}
	args := []interfaces.Expr{}
	for {
		expr := p.parseExpr()
		if expr == nil {
			break
		}
		args = append(args, expr)
		p.expectSeparator(Comma, CloseParen, "argument list")
	}
	p.expect(CloseParen)
	return args
}

func (p *Parser) parseTypeHint() *types.Type {
	switch {
	case p.accept(Bool):
		return types.TypeBool
	case p.accept(Str):
		return types.TypeStr
	case p.accept(Int):
		return types.TypeInt
	case p.accept(Float):
		return types.TypeFloat

	// []<type>
	case p.accept(OpenBracket):
		p.expect(CloseBracket)
		inner := p.parseTypeHint()
		if inner == nil {
			p.errorUnexpected()
		}
		return &types.Type{
			Kind: types.KindList,
			Val:  inner,
		}

	// map{<type>: <type>}
	case p.accept(Map):
		p.expect(OpenBrace)
		key := p.parseTypeHint()
		p.expect(Colon)
		value := p.parseTypeHint()
		p.expect(CloseBrace)

		return &types.Type{
			Kind: types.KindMap,
			Key:  key,
			Val:  value,
		}

	// struct{<ident> => <expr>; ...}
	case p.accept(Struct):
		p.expect(OpenBrace)

		order := []string{}
		typeMap := map[string]*types.Type{}

		for {
			ident := p.text
			if !p.accept(Identifier) {
				break
			}
			typ := p.parseTypeHint()
			if typ == nil {
				p.error(&ExpectedType{ParseError{p.pos()}, ident})
				return nil
			}

			order = append(order, ident)
			typeMap[ident] = typ

			p.expectSeparator(Semicolon, CloseBrace, "struct")
		}
		p.expect(CloseBrace)

		return &types.Type{
			Kind: types.KindStruct,
			Ord:  order,
			Map:  typeMap,
		}

	// func() float or func(bool) str or func(a bool, bb int) float
	case p.accept(Func):
		args := p.parseTypeFuncArgs()
		typ := p.parseTypeHint()
		if typ == nil {
			p.errorUnexpected()
			return nil
		}

		m := make(map[string]*types.Type)
		ord := []string{}
		for i, a := range args {
			if a.Type == nil {
				// at least one is unknown, can't run SetType...
				// this means there is a programming error here!
				// this will ultimately cause a parser error to occur...
				p.error(fmt.Errorf("type is unspecified for arg #%d", i))
				return nil // safety
			}
			name := a.Name
			if name == "" {
				name = util.NumToAlpha(i) // if unspecified...
			}
			if util.StrInList(name, ord) {
				// duplicate arg name used
				// this will ultimately cause a parser error to occur...
				p.error(fmt.Errorf("duplicate arg name of `%s`", name))
				return nil // safety
			}
			m[name] = a.Type
			ord = append(ord, name)
		}

		return &types.Type{
			Kind: types.KindFunc,
			Map:  m,
			Ord:  ord,
			Out:  typ,
		}
	}

	// Never a valid type, so try to generate a helpful error
	switch p.next {
	case Identifier, CapitalizedIdentifier, ResIdentifier:
		p.syntaxError(p.pos(), "illegal token `%s` is not a valid type", p.text)
	}

	return nil
}

func (p *Parser) parseTypeFuncArgs() []*interfaces.Arg {
	args := []*interfaces.Arg{}

	p.expect(OpenParen)
	for {
		// XXX: should we allow named args in the type signature?
		name := ""
		if p.accept(Dollar) {
			name = p.text
			p.expect(Identifier)
		}

		typ := p.parseTypeHint()
		if typ == nil {
			break
		}

		arg := &interfaces.Arg{
			Name: name,
			Type: typ,
		}
		args = append(args, arg)

		p.expectSeparator(Comma, CloseParen, "func type args")
	}
	p.expect(CloseParen)

	return args
}
