package backend

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// The checked cast (S1 plan D4): a decoder generated per type a program
// casts to, calling the runtime's decode.go. A shape's decoder makes its
// struct from an object's keys, keeps the keys the shape doesn't name in the
// struct's extra values, in place in its key table, and fails, naming the
// path, where the value doesn't fit.
//
//	func ts_decode0(v any, at *cakebearrt.DecodePath) *ts_shape1 { … }

// decoding is the emitter's state for decoders: each declared once, by its
// type's Go spelling, and the shapes whose structs hold extra values.
type decoding struct {
	decoders     map[string]string
	decoderDecls []string
	decoded      map[*ir.Shape]bool
}

func (e *emitter) decode(x *ir.Decode) string {
	e.usesRuntime = true
	return e.decoderFor(x.Typ) + "(" + e.expr(x.Value) + ", " + rtPkg + ".DecodeRoot(" + strconv.Quote(x.Cast) + ", " + e.siteArg(x.Span) + "))"
}

// decoderFor is a function of type `func(any, *DecodePath) T` for t.
func (e *emitter) decoderFor(t ir.Type) string {
	switch t.Kind {
	case ir.KindNumber:
		return rtPkg + ".DecodeNumber"
	case ir.KindString:
		return rtPkg + ".DecodeString"
	case ir.KindBoolean:
		return rtPkg + ".DecodeBoolean"
	case ir.KindUnknown:
		return rtPkg + ".DecodeUnknown"
	case ir.KindError:
		return rtPkg + ".DecodeError"
	}
	goT, _ := goType(t)
	if name, ok := e.decoders[goT]; ok {
		return name
	}
	if e.decoders == nil {
		e.decoders = map[string]string{}
	}
	name := "ts_decode" + strconv.Itoa(len(e.decoders))
	e.decoders[goT] = name // before the body: a shape may reach itself
	head := fmt.Sprintf("func %s(v any, at *%s.DecodePath) %s {\n", name, rtPkg, goT)
	var body string
	switch t.Kind {
	case ir.KindMaybe:
		body = fmt.Sprintf("\treturn %s.DecodeMaybe(v, at, %s)\n", rtPkg, e.decoderFor(*t.Elem))
	case ir.KindArray:
		body = fmt.Sprintf("\treturn %s.DecodeArray(v, at, %s)\n", rtPkg, e.decoderFor(*t.Elem))
	case ir.KindObject:
		body = e.shapeDecoder(t.Shape, goT)
	default:
		body = "\tpanic(\"cakebear: no decoder for " + t.String() + "\")\n"
	}
	e.decoderDecls = append(e.decoderDecls, head+body+"}\n\n")
	return name
}

// wantOf is what a type wants, in a mismatch's message.
func wantOf(t ir.Type) string {
	switch t.Kind {
	case ir.KindNumber:
		return "a number"
	case ir.KindString:
		return "a string"
	case ir.KindBoolean:
		return "a boolean"
	case ir.KindArray:
		return "an array"
	case ir.KindObject:
		return "an object"
	case ir.KindError:
		return "an Error"
	case ir.KindMaybe:
		return wantOf(*t.Elem) + ", null or undefined"
	}
	return "a value"
}

// shapeDecoder is the body of s's decoder: its own struct as it is, else
// the struct made from an object's keys in their order.
func (e *emitter) shapeDecoder(s *ir.Shape, goT string) string {
	if e.decoded == nil {
		e.decoded = map[*ir.Shape]bool{}
	}
	e.decoded[s] = true
	layouts := "ts_decodeLayouts" + strconv.Itoa(s.ID)
	e.decoderDecls = append(e.decoderDecls, fmt.Sprintf("var %s = %s.NewDecodeLayouts()\n\n", layouts, rtPkg))
	var b strings.Builder
	fmt.Fprintf(&b, "\tif o, ok := v.(%s); ok {\n\t\treturn o\n\t}\n", goT)
	fmt.Fprintf(&b, "\tsrc := %s.DecodeObject(v, at)\n\to := &%s{}\n", rtPkg, shapeName(s))
	required := false
	for _, f := range s.Fields {
		required = required || !f.Optional
	}
	if required {
		fmt.Fprintf(&b, "\tvar seen [%d]bool\n", len(s.Fields))
	}
	b.WriteString("\tfor i, k := range src.Keys {\n\t\tswitch k {\n")
	for i, f := range s.Fields {
		fmt.Fprintf(&b, "\t\tcase %s:\n\t\t\tstep := at.Step(k)\n\t\t\to.%s = %s(src.Vals[i], &step)\n",
			strconv.Quote(f.Name), fieldName(f.Name), e.decoderFor(f.Type))
		if !f.Optional {
			fmt.Fprintf(&b, "\t\t\tseen[%d] = true\n", i)
		}
	}
	b.WriteString("\t\tdefault:\n\t\t\to.ts_extra = append(o.ts_extra, src.Vals[i])\n\t\t}\n\t}\n")
	for i, f := range s.Fields {
		if !f.Optional {
			fmt.Fprintf(&b, "\tif !seen[%d] {\n\t\t%s.DecodeMissing(at, %s, %s)\n\t}\n", i, rtPkg, strconv.Quote(f.Name), strconv.Quote(wantOf(f.Type)))
		}
	}
	fmt.Fprintf(&b, "\to.%s = %s.DecodeLayout(%s, src.Keys, %d, func(k string) int {\n\t\tswitch k {\n", layoutField, rtPkg, layouts, len(s.Fields))
	for i, f := range s.Fields {
		fmt.Fprintf(&b, "\t\tcase %s:\n\t\t\treturn %d\n", strconv.Quote(f.Name), i)
	}
	b.WriteString("\t\t}\n\t\treturn -1\n\t})\n\treturn o\n")
	return b.String()
}
