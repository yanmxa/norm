package graph

import (
	"context"
	"testing"

	"github.com/yanmxa/metron/internal/codeindex"
)

func TestNamedImportAliasesCompareCanonicalCallees(t *testing.T) {
	for _, tc := range []struct{ path, base, cur string }{
		{"a.py", "from dep import run as execute\ndef caller():\n    return 1\n", "from dep import run as execute\ndef caller():\n    return execute()\n"},
		{"a.ts", "import { run as execute } from './dep';\nfunction caller() { return 1; }\n", "import { run as execute } from './dep';\nfunction caller() { return execute(); }\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			base, err := codeindex.Parse(context.Background(), tc.path, []byte(tc.base))
			if err != nil {
				t.Fatal(err)
			}
			cur, err := codeindex.Parse(context.Background(), tc.path, []byte(tc.cur))
			if err != nil {
				t.Fatal(err)
			}
			before, after := indexedCalls(base), indexedCalls(cur)
			gained := Diff(after["caller"], before["caller"])
			if !gained["run"] || gained["execute"] {
				t.Fatalf("alias gained=%v imports=%+v", gained, cur.Imports)
			}
			if len(Diff(after["caller"], after["caller"])) != 0 {
				t.Fatal("unchanged alias creates new edge")
			}
		})
	}
}
