package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// The M1 acceptance program, as in ~/Desktop/cakebear/targets/hello.ts.
const helloSrc = `import { createServer } from "node:http";

const server = createServer((req, res) => {
  res.setHeader("Content-Type", "text/plain");
  if (req.url === "/health") {
    res.end("ok\n");
  } else {
    res.end("hello from cakebear\n");
  }
});

server.listen(3000, () => {
  console.log("listening on http://localhost:3000");
});
`

func hostCallOf(t *testing.T, x ir.Expr, op ir.HostOp) *ir.HostCall {
	t.Helper()
	call, ok := x.(*ir.HostCall)
	if !ok || call.Op != op {
		t.Fatalf("got %#v, want a %s host call", x, op)
	}
	return call
}

func TestLowerHelloServer(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, helloSrc)
	if len(m.Main) != 2 {
		t.Fatalf("got %d top-level statements, want 2 (the import lowers to nothing)", len(m.Main))
	}

	decl := m.Main[0].(*ir.VarDecl)
	if decl.Type != ir.HostOf(ir.HostHTTPServer) {
		t.Errorf("server has type %s, want Server", decl.Type)
	}
	create := hostCallOf(t, decl.Init, ir.HostCreateServer)
	if create.Recv != nil || len(create.Args) != 1 {
		t.Fatalf("createServer = %#v, want no receiver and one argument", create)
	}
	handler := create.Args[0].(*ir.FuncLit)
	want := ir.FuncType([]ir.Type{ir.HostOf(ir.HostIncomingMessage), ir.HostOf(ir.HostServerResponse)}, ir.Void)
	if !handler.Typ.Equal(want) || handler.Params[0].Name != "req" || handler.Params[1].Name != "res" {
		t.Errorf("handler = %s %+v, want (req: IncomingMessage, res: ServerResponse) => void", handler.Typ, handler.Params)
	}

	set := hostCallOf(t, handler.Body[0].(*ir.ExprStmt).Expr, ir.HostSetHeader)
	if recv, ok := set.Recv.(*ir.Ident); !ok || recv.Name != "res" || len(set.Args) != 2 {
		t.Errorf("setHeader = %#v, want res.setHeader with two arguments", set)
	}
	cond := handler.Body[1].(*ir.If).Cond.(*ir.Binary)
	if url, ok := cond.Left.(*ir.HostProp); !ok || url.Prop != ir.HostURL || url.Typ != ir.String {
		t.Errorf("if condition's left side = %#v, want req.url as a string", cond.Left)
	}
	end := hostCallOf(t, handler.Body[1].(*ir.If).Then[0].(*ir.ExprStmt).Expr, ir.HostEnd)
	if end.Typ != ir.HostOf(ir.HostServerResponse) {
		t.Errorf("res.end returns %s, want ServerResponse (its `this`)", end.Typ)
	}

	listen := hostCallOf(t, m.Main[1].(*ir.ExprStmt).Expr, ir.HostListen)
	if recv, ok := listen.Recv.(*ir.Ident); !ok || recv.Name != "server" || len(listen.Args) != 2 {
		t.Errorf("listen = %#v, want server.listen with a port and a callback", listen)
	}
	if cb, ok := listen.Args[1].(*ir.FuncLit); !ok || !cb.Typ.Equal(ir.FuncType(nil, ir.Void)) {
		t.Errorf("listen's callback = %#v, want a () => void literal", listen.Args[1])
	}
}

// Every import form reaches the same operation. A named import resolves
// through an alias declared in the program's own file, which is the case the
// spike found conversionBrand's check would miss.
func TestHostImportForms(t *testing.T) {
	t.Parallel()

	const body = `((req, res) => { res.end(req.method); });` + "\n"
	for _, tt := range []struct{ name, src string }{
		{"named node:http", `import { createServer } from "node:http"; createServer` + body},
		{"named http", `import { createServer } from "http"; createServer` + body},
		{"renamed", `import { createServer as serve } from "node:http"; serve` + body},
		{"namespace node:http", `import * as http from "node:http"; http.createServer` + body},
		{"namespace http", `import * as http from "http"; http.createServer` + body},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := lowerClean(t, tt.src)
			create := hostCallOf(t, m.Main[0].(*ir.ExprStmt).Expr, ir.HostCreateServer)
			if create.Recv != nil {
				t.Errorf("createServer got receiver %#v; a namespace is not a value", create.Recv)
			}
			end := hostCallOf(t, create.Args[0].(*ir.FuncLit).Body[0].(*ir.ExprStmt).Expr, ir.HostEnd)
			if p, ok := end.Args[0].(*ir.HostProp); !ok || p.Prop != ir.HostMethod {
				t.Errorf("end's argument = %#v, want req.method", end.Args[0])
			}
		})
	}
}

// A chained call keeps its receiver: setHeader returns `this`.
func TestHostChainedCall(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `import { createServer } from "node:http";
createServer((req, res) => { res.setHeader("a", "b").end("c"); });
`)
	handler := hostCallOf(t, m.Main[0].(*ir.ExprStmt).Expr, ir.HostCreateServer).Args[0].(*ir.FuncLit)
	end := hostCallOf(t, handler.Body[0].(*ir.ExprStmt).Expr, ir.HostEnd)
	hostCallOf(t, end.Recv, ir.HostSetHeader)
}

// Something that merely shares a name with node:http's API is not it. Lowering
// asks where each symbol is declared, so these never become host calls.
func TestHostRecognitionIsByDeclaration(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `function createServer(f: () => void): number {
  f();
  return 1;
}
const n: number = createServer(() => {});
`)
	if _, ok := m.Main[0].(*ir.VarDecl).Init.(*ir.Call); !ok {
		t.Errorf("a program's own createServer lowered to %T, want *ir.Call", m.Main[0].(*ir.VarDecl).Init)
	}

	for _, tt := range []struct{ name, src, want string }{
		{"user class with end", `class Mine { end(chunk?: string): Mine { return this; } }
function use(m: Mine): void { m.end("x"); }
`, "parameter m has a type the backend cannot represent yet"},
		{"user interface named ServerResponse", `interface ServerResponse { end(chunk?: string): void; }
function use(r: ServerResponse): void { r.end("x"); }
`, "parameter r has a type the backend cannot represent yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			msgs := joinMsgs(errs)
			if !strings.Contains(msgs, tt.want) || strings.Contains(msgs, "node:http") {
				t.Errorf("errors = %q, want %q and no mention of node:http", msgs, tt.want)
			}
		})
	}
}

// A program may augment ServerResponse. The value is still a real one, but a
// member it added has nothing behind it at runtime, and neither does a new
// overload of a bundled member.
func TestHostAugmentation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"added member", `import { createServer } from "node:http";
declare module "node:http" { interface ServerResponse { extra(): void; } }
createServer((req, res) => { res.extra(); res.end(); });
`, "ServerResponse.extra is not part of cakebear's node:http"},
		{"added overload", `import { createServer } from "node:http";
declare module "node:http" { interface ServerResponse { end(code: number): this; } }
createServer((req, res) => { res.end(5); });
`, "end is augmented outside node:http, which is not supported yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}

func TestHostRefusals(t *testing.T) {
	t.Parallel()

	const imp = "import { createServer } from \"node:http\";\n"
	for _, tt := range []struct{ name, src, want string }{
		{"too many args", imp + `createServer((req, res) => { res.end("a", "b"); });`, "ServerResponse.end takes 0 or 1 arguments, got 2"},
		{"too few args", imp + `createServer();`, "createServer takes 1 argument, got 0"},
		{"function as value", imp + `const cs = createServer;`, "createServer can only be called, not used as a value, yet"},
		{"method as value", imp + `createServer((req, res) => { const e = res.end; });`, "ServerResponse.end can only be called, not used as a value, yet"},
		{"other module", `import { x } from "./lib";`, `imports from "./lib" are not supported yet; cakebear bundles only node:http`},
		{"default import", `import http from "node:http";`, "a default import of node:http is not supported"},
		{"log a host value", imp + `createServer((req, res) => { console.log(res); });`, "console.log cannot print a ServerResponse yet"},
		{"log a function", "const f = (): number => 1;\nconsole.log(f);", "console.log cannot print a function yet"},
		{"=== on functions", "const f = (): number => 1;\nconst g = (): number => 2;\nconst b: boolean = f === g;", "=== is not supported on () => number"},
		{"=== on host values", imp + `createServer((req, res) => { const b: boolean = res === res; });`, "=== is not supported on ServerResponse"},
		{"other property", `const n: number = "abc".length;`, "property access expression is not supported yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			for _, e := range errs {
				if strings.Contains(e.Msg, tt.want) && e.Kind != Unsupported {
					t.Errorf("%q is reported as an error, want a limit of this phase", e.Msg)
				}
			}
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}

// The tables in host.go and the declarations in node-http.d.ts must agree: a
// member declared but not mapped would type-check and then be refused, and one
// mapped but not declared is dead. Both directions are checked.
func TestHostTablesCoverDeclarations(t *testing.T) {
	t.Parallel()

	program, _ := checkSource(t, "export {};\n")
	file := program.GetSourceFile(types.NodeHTTPPath())
	if file == nil {
		t.Fatal("the bundled node:http declarations are not in the program")
	}

	declared := map[hostMember]ast.Kind{}
	for _, stmt := range file.Statements.Nodes {
		if stmt.Kind != ast.KindModuleDeclaration || stmt.Name().Text() != "node:http" {
			continue
		}
		for _, s := range stmt.Body().Statements() {
			switch s.Kind {
			case ast.KindFunctionDeclaration:
				declared[hostMember{"", s.Name().Text()}] = s.Kind
			case ast.KindInterfaceDeclaration:
				for _, member := range s.Members() {
					declared[hostMember{s.Name().Text(), member.Name().Text()}] = member.Kind
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no declarations; the walk is broken, not the tables")
	}

	for m, kind := range declared {
		_, isCall := hostCalls[m]
		_, isProp := hostProps[m]
		switch {
		case kind == ast.KindPropertySignature && !isProp:
			t.Errorf("node-http.d.ts declares property %s, but hostProps does not map it", m)
		case kind != ast.KindPropertySignature && !isCall:
			t.Errorf("node-http.d.ts declares %s, but hostCalls does not map it", m)
		}
	}
	for m := range hostCalls {
		if _, ok := declared[m]; !ok {
			t.Errorf("hostCalls maps %s, which node-http.d.ts does not declare", m)
		}
	}
	for m := range hostProps {
		if _, ok := declared[m]; !ok {
			t.Errorf("hostProps maps %s, which node-http.d.ts does not declare", m)
		}
	}
	for name := range hostTypes {
		if _, ok := hostTypesDeclared(declared)[name]; !ok {
			t.Errorf("hostTypes maps %s, which node-http.d.ts does not declare", name)
		}
	}
}

func hostTypesDeclared(declared map[hostMember]ast.Kind) map[string]bool {
	out := map[string]bool{}
	for m := range declared {
		if m.iface != "" {
			out[m.iface] = true
		}
	}
	return out
}

func joinMsgs(errs []*Error) string {
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Msg)
	}
	return strings.Join(msgs, "\n")
}
