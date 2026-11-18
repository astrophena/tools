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

// Package interpreter runs Starlark programs split across files and packages.
//
// A package has an alias and a loader. Starlark code can refer to a file
// relative to the current module, to "//path" within the current package,
// or to "@alias//path" in another package.
//
// A load caches the result of evaluation unless interrupted. An exec runs a
// script once for its side effects and returns its globals. Only scripts
// started by ExecModule or exec can call exec. A completed load reserves
// the module for future loads. An exec marks it as executed.
//
// If present, @stdlib//builtins.star contributes its public symbols to the
// predeclared globals. Paths in the main package appear as "//path" in errors.
package interpreter

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"go.astrophena.name/base/syncx"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

var (
	// ErrNoModule indicates that a loader could not find a module.
	ErrNoModule = errors.New("no such module")

	// ErrNoPackage indicates that the interpreter has no loader for a package.
	ErrNoPackage = errors.New("no such package")
)

type threadKind int

const (
	threadLoading threadKind = iota + 1
	threadExecing
)

const (
	// MainPkg is the alias for the package with user-supplied code.
	MainPkg = "__main__"
	// StdlibPkg is the alias for the standard library.
	StdlibPkg = "stdlib"
)

const (
	threadCtxKey  = "interpreter.Context"
	threadKindKey = "interpreter.ThreadKind"
	threadModKey  = "interpreter.ModuleKey"
)

// Context returns the context associated with th by the interpreter.
// It panics for threads created outside the interpreter.
func Context(th *starlark.Thread) context.Context {
	if ctx := th.Local(threadCtxKey); ctx != nil {
		return ctx.(context.Context)
	}
	panic("not an Interpreter thread, no context in its locals")
}

// Loader resolves a path relative to its package root. It returns a native
// module's globals or a Starlark module's source. A missing path returns
// [ErrNoModule].
type Loader func(path string) (dict starlark.StringDict, src string, err error)

// Interpreter loads and executes Starlark modules. Configure its fields and
// call [Interpreter.Init] before sharing it between goroutines.
type Interpreter struct {
	// Predeclared holds globals available to all modules.
	Predeclared starlark.StringDict

	// Packages maps package aliases to loaders.
	Packages map[string]Loader

	// Logger handles Starlark print calls. It defaults to writing to stderr.
	Logger func(file string, line int, message string)

	// PreExec runs before a script starts. Calls can nest through exec.
	PreExec func(th *starlark.Thread, module ModuleKey)

	// PostExec runs after a script finishes, including on evaluation errors.
	PostExec func(th *starlark.Thread, module ModuleKey)

	init    atomic.Bool
	globals starlark.StringDict
	modules syncx.Map[ModuleKey, *loadedModule]

	visitedMu sync.Mutex
	visited   []ModuleKey
}

// ModuleKey identifies a file within a package.
type ModuleKey struct {
	Package string
	Path    string
}

// String returns "@pkg//path", or "//path" for the main package.
func (key ModuleKey) String() string {
	pkg := ""
	if key.Package != MainPkg {
		pkg = "@" + key.Package
	}
	return pkg + "//" + key.Path
}

// resolveModule resolves ref against the current module, if any.
func resolveModule(th *starlark.Thread, ref string) (ModuleKey, error) {
	var (
		current ModuleKey
		ok      bool
	)
	if th != nil {
		current, ok = th.Local(threadModKey).(ModuleKey)
	}

	var key ModuleKey
	switch {
	case strings.HasPrefix(ref, "@"):
		pkg, name, found := strings.Cut(ref[1:], "//")
		if !found {
			return ModuleKey{}, errors.New("a module path should be either '//<path>', '<path>' or '@<package>//<path>'")
		}
		if pkg == "" {
			return ModuleKey{}, errors.New("a package alias can't be empty")
		}
		key = ModuleKey{Package: pkg, Path: path.Clean(name)}
	case strings.HasPrefix(ref, "//"):
		if !ok {
			return ModuleKey{}, errors.New("no current package name in thread locals")
		}
		key = ModuleKey{Package: current.Package, Path: path.Clean(ref[2:])}
	default:
		if !ok {
			return ModuleKey{}, errors.New("can't resolve relative module path: no current module information in thread locals")
		}
		key = ModuleKey{Package: current.Package, Path: path.Join(path.Dir(current.Path), ref)}
	}
	if key.Path == ".." || strings.HasPrefix(key.Path, "../") {
		return ModuleKey{}, errors.New("outside the package root")
	}
	return key, nil
}

type loadedModule struct {
	dict starlark.StringDict
	err  error
}

// A nil module is still loading. This marker reserves a module for exec.
var executedModule = &loadedModule{}

// Init installs the predeclared globals and loads @stdlib//builtins.star
// if present. Call it once before loading or executing user modules.
func (intr *Interpreter) Init(ctx context.Context) error {
	if !intr.init.CompareAndSwap(false, true) {
		return errors.New("already initialized")
	}

	intr.globals = make(starlark.StringDict, len(intr.Predeclared)+1)
	maps.Copy(intr.globals, intr.Predeclared)
	intr.globals["exec"] = starlark.NewBuiltin("exec", intr.exec)

	top, err := intr.LoadModule(ctx, StdlibPkg, "builtins.star")
	if err != nil && err != ErrNoModule && err != ErrNoPackage {
		return err
	}
	for k, v := range top {
		if !strings.HasPrefix(k, "_") {
			intr.globals[k] = v
		}
	}
	return nil
}

// LoadModule loads a file from pkg and caches its globals or error.
// An interrupted load can be retried.
// If the module is already being loaded, LoadModule reports a cycle.
func (intr *Interpreter) LoadModule(ctx context.Context, pkg, path string) (starlark.StringDict, error) {
	key, err := resolveModule(nil, "@"+pkg+"//"+path)
	if err != nil {
		return nil, err
	}
	return intr.loadModule(ctx, key)
}

func (intr *Interpreter) loadModule(ctx context.Context, key ModuleKey) (starlark.StringDict, error) {
	if m, found := intr.modules.LoadOrStore(key, nil); found {
		switch m {
		case nil:
			return nil, errors.New("cycle in the module dependency graph")
		case executedModule:
			return nil, errors.New("the module has been exec'ed before and therefore is not loadable")
		default:
			return m.dict, m.err
		}
	}

	m := &loadedModule{err: fmt.Errorf("panic when loading %q", key)}
	defer func() {
		if errors.Is(m.err, context.Canceled) || errors.Is(m.err, context.DeadlineExceeded) {
			intr.modules.Delete(key)
			return
		}
		intr.modules.Store(key, m)
	}()

	m.dict, m.err = intr.runModule(ctx, key, threadLoading)
	return m.dict, m.err
}

// ExecModule executes a script from pkg and returns its globals.
// Each script can be executed once.
func (intr *Interpreter) ExecModule(ctx context.Context, pkg, path string) (starlark.StringDict, error) {
	key, err := resolveModule(nil, "@"+pkg+"//"+path)
	if err != nil {
		return nil, err
	}
	return intr.execModule(ctx, key)
}

func (intr *Interpreter) execModule(ctx context.Context, key ModuleKey) (starlark.StringDict, error) {
	if m, found := intr.modules.LoadOrStore(key, executedModule); found {
		if m == executedModule {
			return nil, errors.New("the module has already been executed, 'exec'-ing same code twice is forbidden")
		}
		return nil, errors.New("the module has been loaded before and therefore is not executable")
	}
	return intr.runModule(ctx, key, threadExecing)
}

// Visited returns the source modules in the order evaluation started.
func (intr *Interpreter) Visited() []ModuleKey {
	intr.visitedMu.Lock()
	defer intr.visitedMu.Unlock()
	return slices.Clone(intr.visited)
}

// LoadSource reads a file from a package. The thread must be evaluating a
// module, and ref uses the same path syntax as load and exec.
func (intr *Interpreter) LoadSource(th *starlark.Thread, ref string) (string, error) {
	if th.Local(threadKindKey) == nil {
		return "", errors.New("wrong kind of thread (not enough information to " +
			"resolve the file reference), only threads that do 'load' and 'exec' can call this function")
	}

	target, err := resolveModule(th, ref)
	if err != nil {
		return "", err
	}

	dict, src, err := intr.invokeLoader(target)
	if err == nil && dict != nil {
		err = errors.New("it is a native Go module")
	}
	if err != nil {
		if err == ErrNoModule {
			err = errors.New("no such file")
		}
		return "", fmt.Errorf("cannot load %s: %s", target, err)
	}
	return src, nil
}

// Thread creates a Starlark thread for calling functions from Go. Builtins can
// retrieve ctx through [Context]. Use [Interpreter.LoadModule] or
// [Interpreter.ExecModule] to evaluate files.
func (intr *Interpreter) Thread(ctx context.Context) *starlark.Thread {
	th := &starlark.Thread{
		Print: func(th *starlark.Thread, msg string) {
			position := th.CallFrame(1).Pos
			if intr.Logger != nil {
				intr.Logger(position.Filename(), int(position.Line), msg)
			} else {
				fmt.Fprintf(os.Stderr, "[%s:%d] %s\n", position.Filename(), position.Line, msg)
			}
		},
	}
	th.SetLocal(threadCtxKey, ctx)
	return th
}

func (intr *Interpreter) exec(th *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var ref string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "module", &ref); err != nil {
		return nil, err
	}
	if th.Local(threadKindKey) != threadExecing {
		return nil, fmt.Errorf("exec %s: forbidden in this context, only exec'ed scripts can exec other scripts", ref)
	}

	key, err := resolveModule(th, ref)
	if err != nil {
		return nil, err
	}
	dict, err := intr.execModule(Context(th), key)
	if evalErr, ok := err.(*starlark.EvalError); ok {
		// Include the inner backtrace in the error for the outer exec call.
		return nil, fmt.Errorf("exec %s failed: %s", ref, evalErr.Backtrace())
	}
	if err != nil {
		return nil, fmt.Errorf("cannot exec %s: %s", ref, err)
	}

	return starlarkstruct.FromStringDict(starlark.String("execed"), dict), nil
}

func (intr *Interpreter) invokeLoader(key ModuleKey) (dict starlark.StringDict, src string, err error) {
	loader, ok := intr.Packages[key.Package]
	if !ok {
		return nil, "", ErrNoPackage
	}
	return loader(key.Path)
}

func (intr *Interpreter) runModule(ctx context.Context, key ModuleKey, kind threadKind) (starlark.StringDict, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dict, src, err := intr.invokeLoader(key)
	switch {
	case err != nil:
		return nil, err
	case dict != nil:
		return dict, nil
	}

	th := intr.Thread(ctx)
	th.Load = func(th *starlark.Thread, module string) (starlark.StringDict, error) {
		key, err := resolveModule(th, module)
		if err != nil {
			return nil, err
		}
		dict, err := intr.loadModule(ctx, key)
		// Preserve the inner backtrace, as in exec.
		if evalErr, ok := err.(*starlark.EvalError); ok {
			err = errors.New(evalErr.Backtrace())
		}
		return dict, err
	}

	th.SetLocal(threadKindKey, kind)
	th.SetLocal(threadModKey, key)

	if kind == threadExecing {
		if intr.PreExec != nil {
			intr.PreExec(th, key)
		}
		if intr.PostExec != nil {
			defer intr.PostExec(th, key)
		}
	}

	intr.visitedMu.Lock()
	intr.visited = append(intr.visited, key)
	intr.visitedMu.Unlock()

	stop := context.AfterFunc(ctx, func() { th.Cancel(ctx.Err().Error()) })
	defer stop()
	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, th, key.String(), src, intr.globals)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	return globals, err
}
