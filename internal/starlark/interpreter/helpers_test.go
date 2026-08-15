// Copyright 2018 The LUCI Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package interpreter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"go.starlark.net/starlark"
)

func deindent(s string) string {
	lines := strings.Split(s, "\n")
	indent := ""
	for _, line := range lines {
		idx := strings.IndexFunc(line, func(r rune) bool { return !unicode.IsSpace(r) })
		if idx != -1 {
			indent = line[:idx]
			break
		}
	}
	if indent == "" {
		return s
	}
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, indent)
	}
	return strings.Join(lines, "\n")
}

type interpreterResult struct {
	keys    []string
	logs    []string
	hooks   []string
	visited []ModuleKey
	err     error
}

func runInterpreter(ctx context.Context, main, stdlib, custom map[string]string, nativeCustom bool) interpreterResult {
	result := interpreterResult{}
	intr := &Interpreter{
		Packages: map[string]Loader{
			MainPkg:   MemoryLoader(main),
			StdlibPkg: MemoryLoader(stdlib),
			"custom":  MemoryLoader(custom),
		},
		Logger: func(file string, line int, message string) {
			result.logs = append(result.logs, fmt.Sprintf("[%s:%d] %s", file, line, message))
		},
		PreExec: func(th *starlark.Thread, module ModuleKey) {
			result.hooks = append(result.hooks, "pre "+module.String())
		},
		PostExec: func(th *starlark.Thread, module ModuleKey) {
			result.hooks = append(result.hooks, "post "+module.String())
		},
	}
	if nativeCustom {
		intr.Packages["custom"] = func(string) (starlark.StringDict, string, error) {
			return starlark.StringDict{}, "", nil
		}
	}
	type contextKey struct{}
	ctx = context.WithValue(ctx, contextKey{}, "context value")
	intr.Predeclared = starlark.StringDict{
		"context_value": starlark.NewBuiltin("context_value", func(th *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.String(Context(th).Value(contextKey{}).(string)), nil
		}),
		"imported_sym": starlark.MakeInt(123),
		"load_src": starlark.NewBuiltin("load_src", func(th *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			src, err := GetThreadInterpreter(th).LoadSource(th, args[0].(starlark.String).GoString())
			return starlark.String(src), err
		}),
	}
	if err := intr.Init(ctx); err != nil {
		result.err = err
		return result
	}
	if _, ok := main["main.star"]; ok {
		dict, err := intr.ExecModule(ctx, MainPkg, "main.star")
		result.err = err
		if err == nil {
			result.keys = make([]string, 0, len(dict))
			for key := range dict {
				result.keys = append(result.keys, key)
			}
			sort.Strings(result.keys)
		}
	}
	result.visited = intr.Visited()
	return result
}

func (r interpreterResult) String() string {
	var out strings.Builder
	writeList := func(name string, values []string) {
		fmt.Fprintf(&out, "%s:\n", name)
		for _, value := range values {
			fmt.Fprintf(&out, "  %s\n", value)
		}
	}
	writeList("keys", r.keys)
	writeList("logs", r.logs)
	writeList("hooks", r.hooks)
	visited := make([]string, len(r.visited))
	for i, module := range r.visited {
		visited[i] = module.String()
	}
	writeList("visited", visited)
	out.WriteString("error:\n")
	if r.err == nil {
		out.WriteString("  <nil>\n")
	} else if evalErr, ok := r.err.(*starlark.EvalError); ok {
		out.WriteString(evalErr.Backtrace())
		out.WriteByte('\n')
	} else {
		fmt.Fprintf(&out, "  %s\n", r.err)
	}
	return out.String()
}
