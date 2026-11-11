package runtime

import (
	"os"
	"strings"
	"sync"
)

// process.env, read-only (S1 plan D8, s1-node R44): the environment the
// program started with. A read is the variable's string, or undefined for
// one that isn't set; an empty one is "". Lowering refuses writes, so the
// table never changes, and a request handler or a spawn may read it.

var env = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		// Node decodes the environment as UTF-8, each ill-formed part one
		// U+FFFD (fetch_response.go's decoder is the same).
		m[decodeUTF8([]byte(name))] = decodeUTF8([]byte(value))
	}
	return m
})

// EnvGet is `process.env[name]`.
func EnvGet(name string) Maybe[string] {
	v, ok := env()[name]
	return OrUndefined(v, ok)
}

// EnvHas is `name in process.env`.
func EnvHas(name string) bool {
	_, ok := env()[name]
	return ok
}
