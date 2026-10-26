package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// consoleLog emits console.log. One value with a printer of its own (a
// number, a string, a boolean, an extension type) calls it, as Phase 1
// always did; anything else goes through the runtime's port of
// util.inspect, Log, which prints objects and arrays, and several values
// separated by spaces.
//
// A maybe goes through Log, which prints its value, or undefined or null. A
// bare null or undefined (ir.NullLit, ir.UndefinedLit) has no value to pass:
// alone it is LogNull() or LogUndefined(), and among other values an empty
// Maybe, which Log prints as the literal it stands for.
func (e *emitter) consoleLog(x *ir.ConsoleLog) string {
	e.usesRuntime = true
	if len(x.Args) == 1 {
		if t := x.Args[0].ExprType(); t == ir.Null || t == ir.Undefined {
			log, _ := logFunc(t)
			return fmt.Sprintf("%s.%s()", rtPkg, log)
		}
		if log, ok := logFunc(x.Args[0].ExprType()); ok {
			return fmt.Sprintf("%s.%s(%s)", rtPkg, log, e.expr(x.Args[0]))
		}
	}
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		switch t := a.ExprType(); {
		case t == ir.Null:
			args[i] = rtPkg + ".NullOf[struct{}]()"
		case t == ir.Undefined:
			args[i] = rtPkg + ".Maybe[struct{}]{}"
		case !inspectable(t, nil):
			return e.cannot(x.Span, "the backend cannot print a "+t.String()+" yet")
		default:
			args[i] = e.expr(a)
		}
	}
	return fmt.Sprintf("%s.Log(%s)", rtPkg, strings.Join(args, ", "))
}

// logFunc names the runtime function that prints a value of type t as
// console.log does. ok is false for a type it has none for: printing
// `undefined` instead would build, and print the wrong thing.
func logFunc(t ir.Type) (string, bool) {
	switch t {
	case ir.Number:
		return "LogNumber", true
	case ir.String:
		return "LogString", true
	case ir.Boolean:
		return "LogBool", true
	case ir.Null:
		return "LogNull", true
	case ir.Undefined:
		return "LogUndefined", true
	case ir.Int32:
		return "LogInt32", true
	case ir.Int64:
		return "LogInt64", true
	case ir.Uint32:
		return "LogUint32", true
	case ir.Uint64:
		return "LogUint64", true
	case ir.Float32:
		return "LogFloat32", true
	case ir.Base64:
		return "LogString", true
	default:
		return "", false
	}
}

// inspectable reports whether the runtime's Inspect can print a value of
// type t: a primitive, or an object, an array or a maybe of them. It can't
// print a function, which has no name in Go for Node's `[Function: name]`,
// nor a host value or a promise. seen guards a shape that refers to itself.
func inspectable(t ir.Type, seen map[*ir.Shape]bool) bool {
	switch t.Kind {
	case ir.KindNumber, ir.KindString, ir.KindBoolean,
		ir.KindInt32, ir.KindInt64, ir.KindUint32, ir.KindUint64, ir.KindFloat32, ir.KindBase64,
		ir.KindUnknown, ir.KindError: // an unknown holds only data (ir.Box)
		return true
	case ir.KindArray, ir.KindMaybe:
		return t.Elem != nil && inspectable(*t.Elem, seen)
	case ir.KindObject:
		if t.Shape == nil {
			return false
		}
		if seen[t.Shape] {
			return true
		}
		if seen == nil {
			seen = map[*ir.Shape]bool{}
		}
		seen[t.Shape] = true
		for _, f := range t.Shape.Fields {
			if !inspectable(f.Type, seen) {
				return false
			}
		}
		return true
	}
	return false
}
