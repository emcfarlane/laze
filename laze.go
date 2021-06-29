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
	//Func func(ctx context.Context) error

	//Label  *Label
	Key   string
	Attrs starlark.StringDict
	Func  *starlark.Function

	// URL: 	https://network.com/file/path
	// ABSOLUTE: 	/root/file/path
	// LOCAL: 	some/file/path
	// RELATIVE: 	./file
	//func func(context.Context, *starlark.Thread) (starlark.Value, error)

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
	WorkDir string // temporary work directoy

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

func (b *Builder) createAction(ctx context.Context, label string) (*Action, error) {

	u, err := url.Parse(label)
	if err != nil {
		return nil, err
	}
	isURL := err == nil && u.Scheme != "" && u.Host != ""
	if isURL || err != nil {
		// TODO: handle URL?
		return nil, fmt.Errorf("error: invalid label %s", label)
	}

	// TODO: label needs to be cleaned...
	label = u.String()
	key := u.Path
	name := path.Base(key)
	dir := path.Dir(key)
	fmt.Println("key", key, "name", name, "dir", dir)

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

	// Load rule.
	r, ok := b.rulesCache[key]
	if !ok {
		if !exists(key) {
			return nil, fmt.Errorf("error: label not found: %s", label)
		}

		// File parameter.
		panic(fmt.Sprintf("TODO: loading files!"))
	}

	// Parse query params
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

	// Find arg deps as attributes.
	var deps []*Action
	for key, arg := range r.args {
		attr := r.attrs[key]

		switch attr.typ {
		case attrTypeLabel:
			label := string(arg.(starlark.String))
			action, err := b.createAction(ctx, label)
			if err != nil {
				fmt.Println("failed because of action!", label)
				return nil, err
			}
			deps = append(deps, action)
		case attrTypeLabelList:
			iter := arg.(starlark.Iterable).Iterate()
			var x starlark.Value
			for iter.Next(&x) {
				label := string(x.(starlark.String))
				action, err := b.createAction(ctx, label)
				if err != nil {
					return nil, err
				}
				deps = append(deps, action)
			}
			iter.Done()
		case attrTypeLabelKeyedStringDict:
			panic("TODO")
		default:
			// do nothing
		}
	}

	// TODO: build action list...
	action := &Action{
		Deps:  deps,
		Key:   key,
		Attrs: r.args,
		Func:  r.impl,
	}

	if b.actionCache == nil {
		b.actionCache = make(map[string]*Action)
	}
	b.actionCache[label] = action
	return action, nil
}

func (b *Builder) init(ctx context.Context) error {
	tmpDir, err := ioutil.TempDir("", "laze")
	if err != nil {
		return err
	}
	b.WorkDir = tmpDir
	return nil
}

func (b *Builder) cleanup() error {
	if b.WorkDir != "" {
		start := time.Now()
		for {
			err := os.RemoveAll(b.WorkDir)
			if err == nil {
				break
			}

			// On some configurations of Windows, directories containing executable
			// files may be locked for a while after the executable exits (perhaps
			// due to antivirus scans?). It's probably worth a little extra latency
			// on exit to avoid filling up the user's temporary directory with leaked
			// files. (See golang.org/issue/30789.)
			if runtime.GOOS != "windows" || time.Since(start) >= 500*time.Millisecond {
				return fmt.Errorf("failed to remove work dir: %v", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return nil
}

func (b *Builder) Build(ctx context.Context, label string) (*Action, error) {

	//
	root, err := b.createAction(ctx, label)
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
				if a.Func != nil && !a.Failed {

					args := starlark.Tuple([]starlark.Value{
						newCtxModule(ctx, a.Key, a.Attrs),
					})

					value, err = starlark.Call(thread, a.Func, args, nil)
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

/*
// KO run.
type run struct {
	name       string
	env        []string
	deps, outs []string

	//args []string

	//tempDir bool
}

func run(ctx context.Context, ip string, dir string, platform v1.Platform, disableOptimizations bool) (string, error) {
	tmpDir, err := ioutil.TempDir("", "laze")
	if err != nil {
		return "", err
	}
	file := filepath.Join(tmpDir, "out")

	args := make([]string, 0, 7)
	args = append(args, "build")
	if disableOptimizations {
		// Disable optimizations (-N) and inlining (-l).
		args = append(args, "-gcflags", "all=-N -l")
	}
	args = append(args, "-o", file)
	args = addGo113TrimPathFlag(args)
	args = append(args, ip)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir

	// Last one wins
	defaultEnv := []string{
		"CGO_ENABLED=0",
		"GOOS=" + platform.OS,
		"GOARCH=" + platform.Architecture,
	}

	if strings.HasPrefix(platform.Architecture, "arm") && platform.Variant != "" {
		goarm, err := getGoarm(platform)
		if err != nil {
			return "", fmt.Errorf("goarm failure for %s: %v", ip, err)
		}
		if goarm != "" {
			defaultEnv = append(defaultEnv, "GOARM="+goarm)
		}
	}

	cmd.Env = append(defaultEnv, os.Environ()...)

	var output bytes.Buffer
	cmd.Stderr = &output
	cmd.Stdout = &output

	log.Printf("Building %s for %s", ip, platformToString(platform))
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		log.Printf("Unexpected error running \"go build\": %v\n%v", err, output.String())
		return "", err
	}
	return file, nil
}
*/