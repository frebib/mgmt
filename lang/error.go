package lang

import (
	"fmt"
	"strings"

	"github.com/purpleidea/mgmt/lang/interfaces"
)

type ParseError struct {
	Pos interfaces.Pos
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("at %s", e.Pos.String())
}

type SyntaxError struct {
	ParseError
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s at %s", e.Message, e.Pos.String())
}

func (e *SyntaxError) Unwrap() error {
	return &e.ParseError
}

type UnexpectedToken struct {
	ParseError
	Token Token
	Text  string
}

func (e *UnexpectedToken) Error() string {
	return fmt.Sprintf("unexpected token `%s` at %s", e.Text, e.Pos.String())
}

func (e *UnexpectedToken) Unwrap() error {
	return &e.ParseError
}

type ExpectedType struct {
	ParseError
	Ident string
}

func (e *ExpectedType) Error() string {
	return fmt.Sprintf("expected type hint for '%s' at %s", e.Ident, e.Pos.String())
}

func (e *ExpectedType) Unwrap() error {
	return &e.ParseError
}

type ExpectedSeparator struct {
	ParseError
	Separator Token
	Context   string
}

func (e *ExpectedSeparator) Error() string {
	expected := strings.ToLower(e.Separator.String())
	context := ""
	if e.Context != "" {
		context = " " + e.Context
	}
	return fmt.Sprintf("missing %v%s at %s", expected, context, e.Pos.String())
}

func (e *ExpectedSeparator) Unwrap() error {
	return &e.ParseError
}
