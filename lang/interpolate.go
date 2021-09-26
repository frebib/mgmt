// Mgmt
// Copyright (C) 2013-2021+ James Shubin and the project contributors
// Written by James Shubin <james@shubin.ca> and the project contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

//go:build !interpolatehil
// +build !interpolatehil

package lang // TODO: move this into a sub package of lang/$name?

import (
	"fmt"

	"github.com/purpleidea/mgmt/lang/interfaces"
	"github.com/purpleidea/mgmt/lang/interpolate"
	"github.com/purpleidea/mgmt/util/errwrap"
)

// InterpolateStr interpolates a string and returns the representative AST. It
// uses the ragel parser to perform the string interpolation.
func InterpolateStr(str string, pos interfaces.Pos) (interfaces.Expr, error) {
	sequence, err := interpolate.Parse(str)
	if err != nil {
		return nil, errwrap.Wrapf(err, "parser failed")
	}

	exprs := []interfaces.Expr{}
	for _, term := range sequence {

		switch t := term.(type) {
		case interpolate.Literal:
			expr := &ExprStr{
				V: t.Value,
			}
			exprs = append(exprs, expr)

		case interpolate.Variable:
			expr := &ExprVar{
				Name: t.Name,
			}
			exprs = append(exprs, expr)
		default:
			return nil, fmt.Errorf("unknown term (%T): %+v", t, t)
		}
	}

	// If we didn't find anything of value, we got an empty string...
	if len(sequence) == 0 && str == "" { // be doubly sure...
		expr := &ExprStr{
			V: "",
		}
		exprs = append(exprs, expr)
	}

	// The parser produces non-optimal results where two strings are next to
	// each other, when they could be statically combined together.
	simplified, err := simplifyExprList(exprs)
	if err != nil {
		return nil, errwrap.Wrapf(err, "expr list simplify failed")
	}

	result, err := concatExprListIntoCall(simplified)
	if err != nil {
		return nil, errwrap.Wrapf(err, "concat expr list failed")
	}

	return result, nil
}

// concatExprListIntoCall takes a list of expressions, and combines them into an
// expression which ultimately concatenates them all together with a + operator.
// TODO: this assumes they're all strings, do we need to watch out for int's?
func concatExprListIntoCall(exprs []interfaces.Expr) (interfaces.Expr, error) {
	if len(exprs) == 0 {
		return nil, fmt.Errorf("empty list")
	}

	operator := &ExprStr{
		V: "+", // for PLUS this is a `+` character
	}

	if len(exprs) == 1 {
		return exprs[0], nil // just return self
	}
	//if len(exprs) == 1 {
	//	arg := exprs[0]
	//	emptyStr := &ExprStr{
	//		V: "", // empty str
	//	}
	//	return &ExprCall{
	//		Name: operatorFuncName, // concatenate the two strings with + operator
	//		Args: []interfaces.Expr{
	//			operator, // operator first
	//			arg,      // string arg
	//			emptyStr,
	//		},
	//	}, nil
	//}

	head, tail := exprs[0], exprs[1:]

	grouped, err := concatExprListIntoCall(tail)
	if err != nil {
		return nil, err
	}

	return &ExprCall{
		// NOTE: if we don't set the data field we need Init() called on it!
		Name: operatorFuncName, // concatenate the two strings with + operator
		Args: []interfaces.Expr{
			operator, // operator first
			head,     // string arg
			grouped,  // nested function call which returns a string
		},
	}, nil
}

// simplifyExprList takes a list of *ExprStr and *ExprVar and groups the
// sequential *ExprStr's together. If you pass it a list of Expr's that contains
// a different type of Expr, then this will error.
func simplifyExprList(exprs []interfaces.Expr) ([]interfaces.Expr, error) {
	last := false
	result := []interfaces.Expr{}

	for _, x := range exprs {
		switch v := x.(type) {
		case *ExprStr:
			if !last {
				last = true
				result = append(result, x)
				continue
			}

			// combine!
			expr := result[len(result)-1] // there has to be at least one
			str, ok := expr.(*ExprStr)
			if !ok {
				// programming error
				return nil, fmt.Errorf("unexpected type (%T)", expr)
			}
			str.V += v.V // combine!
			//last = true // redundant, it's already true
			// ... and don't append, we've combined!

		case *ExprVar:
			last = false // the next one can't combine with me
			result = append(result, x)

		default:
			return nil, fmt.Errorf("unsupported type (%T)", x)
		}
	}

	return result, nil
}
