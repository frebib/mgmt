package interfaces

import (
	"fmt"
)

// Pos represents a position in the source code.
// TODO: consider expanding with range characteristics.
type Pos struct {
	Line     int    // line number starting at 1
	Column   int    // column number starting at 1
	Length	 int    // length in characters from start column
	Filename string // optional source filename, if known
}

func (p Pos) String() string {
	if p.Filename == "" {
		if p.Line == 0 && p.Column == 0 {
			return ""
		} else if p.Column == 0 {
			return fmt.Sprintf("line %d", p.Line)
		}
		return fmt.Sprintf("line %d, col %d", p.Line, p.Column)
	}
	if p.Column == 0 {
		return fmt.Sprintf("line %d in %s", p.Line, p.Filename)
	}
	return fmt.Sprintf("line %d, col %d in %s", p.Line, p.Column, p.Filename)
}
