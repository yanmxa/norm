package codeindex

import (
	"context"
	"strings"
	"testing"
)

func TestParseLanguages(t *testing.T) {
	cases := []struct {
		path, src, qualified, signature string
		calls                           []string
		imports                         []Import
	}{
		{"x.go", `package sample
import fmt "fmt"
type Box[T any] struct{}
func (b *Box[T]) Run(ctx string, fn func(int) error) (string, error) {
 fmt.Println(ctx); helper(); fn(1); b.Other(); return "", nil
}`, "Box::Run", "(ctx string, fn func(int) error) (string, error)", []string{"fmt.Println", "helper"}, []Import{{Alias: "fmt", Path: "fmt"}}},
		{"x.js", `import * as api from './api'; import {help as assist} from './help';
export const run = (arg) => { api.send(arg); assist(); arg(); obj.method(); }`, "run", "(arg)", []string{"api.send", "assist"}, []Import{{Alias: "api", Path: "./api"}, {Alias: "assist", Path: "./help", Name: "help"}}},
		{"x.ts", `import {help} from './help'; export function run(arg: number): number { help(); return arg + 1; }`, "run", "(arg: number): number", []string{"help"}, []Import{{Alias: "help", Path: "./help", Name: "help"}}},
		{"x.tsx", `import * as React from 'react'; export const run = (arg: string) => { React.createElement('div'); return <div>{arg}</div>; };`, "run", "(arg: string)", []string{"React.createElement"}, []Import{{Alias: "React", Path: "react"}}},
		{"x.py", `import api as service
from helpers import help as assist
def run(arg: str) -> str:
    service.send(arg)
    assist()
    arg()
    obj.method()
    return arg
`, "run", "(arg: str) -> str", []string{"service.send", "assist"}, []Import{{Alias: "service", Path: "api"}, {Alias: "assist", Path: "helpers", Name: "help"}}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			f, err := Parse(context.Background(), tc.path, []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(f.Symbols) != 1 {
				t.Fatalf("symbols: %+v", f.Symbols)
			}
			s := f.Symbols[0]
			if s.Qualified != tc.qualified || s.Signature != tc.signature {
				t.Errorf("symbol: %+v", s)
			}
			got := callNames(s)
			if strings.Join(got, ",") != strings.Join(tc.calls, ",") {
				t.Errorf("calls %v want %v", got, tc.calls)
			}
			if len(f.Imports) != len(tc.imports) {
				t.Fatalf("imports: %+v", f.Imports)
			}
			for i := range tc.imports {
				if f.Imports[i] != tc.imports[i] {
					t.Errorf("import %d = %+v want %+v", i, f.Imports[i], tc.imports[i])
				}
			}
			if s.StartLine < 1 || s.EndLine < s.StartLine || s.ID == "" {
				t.Errorf("invalid locations/ID: %+v", s)
			}
		})
	}
}
func callNames(s Symbol) []string {
	var names []string
	for _, c := range s.Calls {
		name := c.Name
		if c.Qualifier != "" {
			name = c.Qualifier + "." + name
		}
		names = append(names, name)
	}
	return names
}
func TestShadowingAndNested(t *testing.T) {
	cases := []struct {
		path, src string
		want      map[string]string
	}{
		{"x.go", `package x
import api "api"
func run(api interface{}, helper func()) { helper(); api.Send(); local := helper; local(); other() }`, map[string]string{"run": "other"}},
		{"x.js", `import * as api from './api';
function run(api, helper) { helper(); api.send(); const local = helper; local(); other(); function nested() { helper(); other(); } }
class Box { run(arg) { arg(); other(); } }`, map[string]string{"run": "other", "run::nested": "other", "Box::run": "other"}},
		{"x.ts", `import * as api from './api'; function run(api: any, helper: () => void) { api.send(); helper(); other(); }`, map[string]string{"run": "other"}},
		{"x.py", `import api
def run(api, helper):
    helper()
    api.send()
    local = helper
    local()
    other()
    def nested():
        helper()
        other()
class Box:
    def run(self, arg):
        self.other()
        arg()
        other()
`, map[string]string{"run": "other", "run::nested": "other", "Box::run": "other"}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			f, err := Parse(context.Background(), tc.path, []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(f.Symbols) != len(tc.want) {
				t.Fatalf("symbols: %+v", f.Symbols)
			}
			for _, s := range f.Symbols {
				want, ok := tc.want[s.Qualified]
				if !ok || strings.Join(callNames(s), ",") != want {
					t.Errorf("%s calls %v want %s", s.Qualified, callNames(s), want)
				}
			}
		})
	}
}
func TestInvalidAndCanceled(t *testing.T) {
	for _, tc := range []struct{ path, src string }{{"x.go", "package x;func {"}, {"x.js", "function broken( {"}, {"x.ts", "function broken(: {"}, {"x.py", "def broken(\n"}, {"x.txt", "text"}} {
		if _, err := Parse(context.Background(), tc.path, []byte(tc.src)); err == nil {
			t.Errorf("accepted %s", tc.path)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(ctx, "x.js", nil); err != context.Canceled {
		t.Errorf("cancel: %v", err)
	}
}
func TestFingerprintAndIDs(t *testing.T) {
	for _, tc := range []struct{ path, a, b, c string }{
		{"x.go", "package x;func run(a int) int { b:=a+1; b=b*2; b=b-3; return b+4+5 }", "package x\n// comment\nfunc run(a int) int {\n b := a + 1 // hello\n b = b * 2\n b = b - 3\n return b + 4 + 5\n}", "package x;func run(a int) int { b:=a+2; b=b*2; b=b-3; return b+4+5 }"},
		{"x.js", "function run(a) { let b=a+1; b=b*2; b=b-3; return b+4+5; }", "// comment\nfunction run(a) { let b = a + 1; /* hello */ b = b * 2; b = b - 3; return b + 4 + 5; }", "function run(a) { let b=a+2; b=b*2; b=b-3; return b+4+5; }"},
		{"x.py", "def run(a):\n b=a+1\n b=b*2\n b=b-3\n return b+4+5\n", "# comment\ndef run(a):\n b = a + 1 # hello\n b = b * 2\n b = b - 3\n return b + 4 + 5\n", "def run(a):\n b=a+2\n b=b*2\n b=b-3\n return b+4+5\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			parse := func(src string) Symbol {
				f, err := Parse(context.Background(), tc.path, []byte(src))
				if err != nil {
					t.Fatal(err)
				}
				return f.Symbols[0]
			}
			a, b, c := parse(tc.a), parse(tc.b), parse(tc.c)
			if a.Fingerprint == "" || a.Fingerprint != b.Fingerprint || a.Fingerprint == c.Fingerprint {
				t.Errorf("fingerprints: %q %q %q", a.Fingerprint, b.Fingerprint, c.Fingerprint)
			}
			if a.ID != b.ID || a.ID != c.ID {
				t.Errorf("unstable IDs: %s %s %s", a.ID, b.ID, c.ID)
			}
		})
	}
}
func TestExports(t *testing.T) {
	f, err := Parse(context.Background(), "x.ts", []byte(`export function publicFn() {} function privateFn() {} const explicit = () => {}; export {explicit}; export default function defaultFn() {}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range f.Symbols {
		want := s.Name == "publicFn" || s.Name == "explicit"
		if s.Exported != want {
			t.Errorf("export %s: %v", s.Name, s.Exported)
		}
	}
}
func TestConservativeBindings(t *testing.T) {
	for _, tc := range []struct {
		path, src string
		unknown   int
	}{
		{"x.js", `import {help} from './help'; function run({helper: local}, fallback = help) { local(); fallback(); help = fallback; help(); other(); }`, 3},
		{"x.py", "from helpers import help\ndef run(helper=help):\n helper()\n [help() for help in things]\n import api\n api.send()\n other()\n", 3},
		{"x.go", `package x; func run[T any](x T, helper func()) T { helper(); obj.Method(); other(); return x }`, 2},
	} {
		t.Run(tc.path, func(t *testing.T) {
			f, err := Parse(context.Background(), tc.path, []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			s := f.Symbols[0]
			if strings.Join(callNames(s), ",") != "other" || s.Unresolved != tc.unknown {
				t.Errorf("symbol: %+v", s)
			}
			if tc.path == "x.go" && s.Signature != "(x T, helper func()) T" {
				t.Error(s.Signature)
			}
		})
	}
}
func TestFingerprintStructureAndTrivial(t *testing.T) {
	parse := func(src string) Symbol {
		f, err := Parse(context.Background(), "x.py", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		return f.Symbols[0]
	}
	a := parse("def run(a):\n if a:\n  b=a+1\n  b=b*2\n  b=b-3\n  return b+4+5\n")
	b := parse("def run(a):\n if a:\n  b=a+1\n b=b*2\n b=b-3\n return b+4+5\n")
	if a.Fingerprint == "" || a.Fingerprint == b.Fingerprint {
		t.Fatal("indentation structure lost")
	}
	if parse("def run():\n pass\n").Fingerprint != "" {
		t.Fatal("trivial fingerprint retained")
	}
}
func TestCallableForms(t *testing.T) {
	for _, tc := range []struct {
		path, src string
		names     []string
	}{
		{"x.js", `const outer = function internal() { const nested = () => target(); nested(); }; const box = { run() { target(); } }; class Box { arrow = () => target(); }`, []string{"outer", "outer::nested", "box::run", "Box::arrow"}},
		{"x.py", "class Box:\n @staticmethod\n def run():\n  target()\n", []string{"Box::run"}},
	} {
		f, err := Parse(context.Background(), tc.path, []byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range f.Symbols {
			names = append(names, s.Qualified)
		}
		if strings.Join(names, ",") != strings.Join(tc.names, ",") {
			t.Errorf("%s: %v", tc.path, names)
		}
	}
}
func TestSupported(t *testing.T) {
	for _, p := range []string{"a.go", "a.js", "a.jsx", "a.mjs", "a.cjs", "a.ts", "a.tsx", "a.py", "a.pyi"} {
		if !Supported(p) {
			t.Error(p)
		}
	}
	if Supported("a.json") {
		t.Error("json supported")
	}
}
