// laze builds things.
package laze

import (
	"container/heap"
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"runtime"
	"time"

	"github.com/emcfarlane/starlarkassert"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

var (
	BuildP = runtime.GOMAXPROCS(0) // -p flag
)

// Laze takes lots of inspiration from Bazel and the Go build tool.
// https://github.com/golang/go/blob/master/src/cmd/go/internal/work/action.go

// An Action represents a single action in the action graph.
type Action struct {
	Deps []*Action // Actions this action depends on.

	Key string // Key is the labels path.

	// REMOTE: 	http://network.com/file/path
	// ABSOLUTE: 	file:///root/file/path
	// LOCAL: 	file://local/file/path
	// RELATIVE: 	file ./file ../file
	Func func(*starlark.Thread) (starlark.Value, error)

	triggers []*Action // reverse of deps
	pending  int       // number of actions pending
	priority int       // relative execution priority

	// Results
	Value     starlark.Value // caller value provider
	Error     error          // caller error
	Failed    bool           // whether the action failed
	TimeReady time.Time
	TimeDone  time.Time
}

// Struct is a helper for strarlarkstruct.
type Struct struct {
	*starlarkstruct.Struct
}

// loadStructValue gets the value and checks the constructor type matches.
func (a *Action) loadStructValue(constructor starlark.Value) (Struct, error) {
	if a.Value == nil {
		return Struct{}, fmt.Errorf("missing struct value")
	}
	s, ok := a.Value.(*starlarkstruct.Struct)
	if !ok {
		return Struct{}, fmt.Errorf("invalid type: %T", a.Value)
	}
	// Constructor values must be comparable
	if c := s.Constructor(); c != constructor {
		return Struct{}, fmt.Errorf("invalid struct type: %s", c)
	}
	return Struct{s}, nil
}

func (s Struct) AttrString(name string) (string, error) {
	x, err := s.Attr(name)
	if err != nil {
		return "", err
	}
	v, ok := starlark.AsString(x)
	if !ok {
		return "", fmt.Errorf("attr %q not a string", name)
	}
	return v, nil
}

// FailureErr is a DFS on the failed action, returns nil if not failed.
func (a *Action) FailureErr() error {
	if !a.Failed {
		return nil
	}
	if a.Error != nil {
		return a.Error
	}
	for _, a := range a.Deps {
		if err := a.FailureErr(); err != nil {
			return err
		}
	}
	// TODO: panic?
	return fmt.Errorf("unknown failure: %s", a.Key)
}

// An actionQueue is a priority queue of actions.
type actionQueue []*Action

// Implement heap.Interface
func (q *actionQueue) Len() int           { return len(*q) }
func (q *actionQueue) Swap(i, j int)      { (*q)[i], (*q)[j] = (*q)[j], (*q)[i] }
func (q *actionQueue) Less(i, j int) bool { return (*q)[i].priority < (*q)[j].priority }
func (q *actionQueue) Push(x interface{}) { *q = append(*q, x.(*Action)) }
func (q *actionQueue) Pop() interface{} {
	n := len(*q) - 1
	x := (*q)[n]
	*q = (*q)[:n]
	return x
}

func (q *actionQueue) push(a *Action) {
	a.TimeReady = time.Now()
	heap.Push(q, a)
}

func (q *actionQueue) pop() *Action {
	return heap.Pop(q).(*Action)
}

// A Builder holds global state about a build.
type Builder struct {
	Dir    string // directory
	tmpDir string // temporary directory TODO: caching tmp dir?

	actionCache map[string]*Action // a cache of already-constructed actions
	rulesCache  map[string]*rule   // a cache of created rules
	moduleCache map[string]bool    // a cache of modules
	//filesCache  map[string]bool    // a cache of files

}

// TODO: how globals work?
var globals = starlark.StringDict{
	"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
}

// load a starlark module.
func (b *Builder) load(thread *starlark.Thread, module string) (starlark.StringDict, error) {

	switch module {
	case "assert.star":
		return starlarkassert.LoadAssertModule()
	case "rule.star":
		return starlark.StringDict{
			"rule": starlark.NewBuiltin("rule", b.rule),
			"attr": newAttrModule(),
		}, nil
	}

	fmt.Println("trying to load", module)
	src, err := ioutil.ReadFile(module)
	if err != nil {
		return nil, err
	}

	// Set module name on each thread.
	if module, ok := thread.Local("module").(string); ok {
		defer thread.SetLocal("module", module)
	}
	thread.SetLocal("module", module)
	fmt.Println("setting module", module)

	return starlark.ExecFile(thread, module, src, globals)
}

func (b *Builder) addAction(label string, action *Action) *Action {
	if b.actionCache == nil {
		b.actionCache = make(map[string]*Action)
	}
	b.actionCache[label] = action
	return action
}

func parseLabel(label string, dir string) (*url.URL, error) {
	u, err := url.Parse(label)
	if err != nil {
		return nil, err
	}
	fmt.Printf("%+v\n", *u)
	if err != nil {
		return nil, fmt.Errorf("error: invalid label %s", label)
	}
	if u.Scheme == "" {
		u.Scheme = "file"
		if len(u.Path) > 0 && u.Path[0] != '/' {
			u.Path = path.Join(dir, u.Path)
		}
	}
	if u.Scheme != "file" {
		return nil, fmt.Errorf("error: unknown scheme %s", u.Scheme)
	}

	// HACK: host -> path
	if u.Scheme == "file" && u.Host != "" {
		u.Path = path.Join(u.Host, u.Path)
		u.Host = ""
	}
	return u, nil
}

func (b *Builder) createAction(ctx context.Context, u *url.URL) (*Action, error) {

	// TODO: validate URL type
	// TODO: label needs to be cleaned...
	label := u.String()
	key := u.Path
	name := path.Base(key)
	dir := path.Dir(key)
	fmt.Println("u", u.String(), "key", key, "name", name, "dir", dir)

	if action, ok := b.actionCache[label]; ok {
		return action, nil
	}

	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}

	if !fi.Mode().IsDir() {
		return nil, fmt.Errorf("invalid path %v", dir)
	}
	// Load module.
	module := path.Join(dir, "BUILD.star")
	exists := func(name string) bool {
		if _, err := os.Stat(name); err != nil {
			if os.IsNotExist(err) {
				return false
			}
		}
		return true
	}

	if ok := b.moduleCache[module]; !ok && exists(module) {
		thread := &starlark.Thread{Load: b.load}
		d, err := b.load(thread, module)
		if err != nil {
			return nil, err
		}

		// rule will inject the value?
		for key, val := range d {
			fmt.Println(" - ", key, val)
		}
		if b.moduleCache == nil {
			b.moduleCache = make(map[string]bool)
		}
		b.moduleCache[module] = true
	}

	// Load rule, or file.
	r, ok := b.rulesCache[key]
	if !ok {
		fi, err := os.Stat(key)
		if err != nil {
			return nil, fmt.Errorf("error: label not found: %s", label)
		}

		// File param.
		return b.addAction(label, &Action{
			Deps: nil,
			Key:  key,
			Func: func(*starlark.Thread) (starlark.Value, error) {
				return newFile(key, fi)
			},
		}), nil
	}

	// Parse query params, override args.
	for key, vals := range u.Query() {
		attr, ok := r.attrs[key]
		if !ok {
			return nil, fmt.Errorf("error: unknown query param: %s", key)
		}

		switch attr.typ {
		case attrTypeString:
			if len(vals) > 1 {
				return nil, fmt.Errorf("error: unexpected number of params: %v", vals)
			}
			s := vals[0]
			// TODO: attr validation?
			r.args[key] = starlark.String(s)

		default:
			panic("TODO: query parsing")
		}
	}

	// TODO: caching the ins & outs?
	// should caching be done on the action execution?

	attrs := make(starlark.StringDict)

	// Find arg deps as attributes and resolve args to targets.
	var deps []*Action
	for key, arg := range r.args {
		attr := r.attrs[key]

		switch attr.typ {
		case attrTypeLabel:
			label := string(arg.(starlark.String))
			u, err := parseLabel(label, dir)
			if err != nil {
				return nil, err
			}
			action, err := b.createAction(ctx, u)
			if err != nil {
				return nil, fmt.Errorf("action creation: %w", err)
			}
			deps = append(deps, action)
			attrs[key] = newTarget(label, action)

		case attrTypeLabelList:
			var elems []starlark.Value
			iter := arg.(starlark.Iterable).Iterate()
			var x starlark.Value
			for iter.Next(&x) {
				label := string(x.(starlark.String))
				u, err := parseLabel(label, dir)
				if err != nil {
					return nil, err
				}
				action, err := b.createAction(ctx, u)
				if err != nil {
					return nil, fmt.Errorf("action creation: %w", err)
				}
				deps = append(deps, action)
				elems = append(elems, newTarget(
					label, action,
				))
			}
			iter.Done()
			attrs[key] = starlark.NewList(elems)

		case attrTypeLabelKeyedStringDict:
			panic("TODO")

		default:
			// copy
			attrs[key] = arg
		}
	}

	return b.addAction(label, &Action{
		Deps: deps,
		Key:  key,
		Func: func(thread *starlark.Thread) (starlark.Value, error) {
			args := starlark.Tuple{
				newCtxModule(ctx, key, b.Dir, b.tmpDir, attrs),
			}
			return starlark.Call(thread, r.impl, args, nil)
		},
	}), nil
}

// TODO: caching with tmp dir.
func (b *Builder) init(ctx context.Context) error {
	tmpDir, err := ioutil.TempDir("", "laze")
	if err != nil {
		return err
	}
	b.tmpDir = tmpDir
	return nil
}

func (b *Builder) cleanup() error {
	//if b.WorkDir != "" {
	//	start := time.Now()
	//	for {
	//		err := os.RemoveAll(b.WorkDir)
	//		if err == nil {
	//			break
	//		}
	//		// On some configurations of Windows, directories containing executable
	//		// files may be locked for a while after the executable exits (perhaps
	//		// due to antivirus scans?). It's probably worth a little extra latency
	//		// on exit to avoid filling up the user's temporary directory with leaked
	//		// files. (See golang.org/issue/30789.)
	//		if runtime.GOOS != "windows" || time.Since(start) >= 500*time.Millisecond {
	//			return fmt.Errorf("failed to remove work dir: %v", err)
	//		}
	//		time.Sleep(5 * time.Millisecond)
	//	}
	//}
	return nil
}

func (b *Builder) Build(ctx context.Context, args []string, label string) (*Action, error) {
	// TODO: handle args.

	u, err := parseLabel(label, ".")
	if err != nil {
		return nil, err
	}

	// create action
	root, err := b.createAction(ctx, u)
	if err != nil {
		return nil, err
	}

	b.Do(ctx, root)
	fmt.Println("completed action", root.Key, root.Value, root.Error)

	return root, nil
}

// actionList returns the list of actions in the dag rooted at root
// as visited in a depth-first post-order traversal.
func actionList(root *Action) []*Action {
	seen := map[*Action]bool{}
	all := []*Action{}
	var walk func(*Action)
	walk = func(a *Action) {
		if seen[a] {
			return
		}
		seen[a] = true
		for _, a1 := range a.Deps {
			walk(a1)
		}
		all = append(all, a)
	}
	walk(root)
	return all
}

func (b *Builder) Do(ctx context.Context, root *Action) {

	// Build list of all actions, assigning depth-first post-order priority.
	all := actionList(root)
	for i, a := range all {
		a.priority = i
	}

	var (
		readyN int
		ready  actionQueue
	)

	// Initialize per-action execution state.
	for _, a := range all {
		for _, a1 := range a.Deps {
			a1.triggers = append(a1.triggers, a)
		}
		a.pending = len(a.Deps)
		if a.pending == 0 {
			ready.push(a)
			readyN++
		}
	}

	// Now we have the list of actions lets run them...
	//
	par := BuildP
	jobs := make(chan *Action, par)
	done := make(chan *Action, par)
	workerN := par
	for i := 0; i < par; i++ {
		go func() {
			thread := &starlark.Thread{}

			for a := range jobs {
				// Run job.
				var value starlark.Value = starlark.None
				var err error
				fmt.Println("RUNNING ACTION", a.Key, "failed?", a.Failed)
				if a.Func != nil && !a.Failed {
					value, err = a.Func(thread)
				}
				if err != nil {
					// Log?
					a.Failed = true
					a.Error = err
				}
				a.Value = value
				a.TimeDone = time.Now()

				done <- a
			}
		}()
	}
	defer close(jobs)

	for i := len(all); i > 0; i-- {
		// Send ready actions to available workers via the jobs queue.
		for readyN > 0 && workerN > 0 {
			jobs <- ready.pop()
			readyN--
			workerN--
		}

		fmt.Println("waiting for action")
		// Wait for completed actions via the done queue.
		a := <-done
		fmt.Println("got action")
		workerN++

		for _, a0 := range a.triggers {
			if a.Failed {
				a0.Failed = true
			}
			if a0.pending--; a0.pending == 0 {
				ready.push(a0)
				readyN++
			}
		}
	}
	fmt.Println("completed do")
}
