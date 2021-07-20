package laze

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// File
const fileConstructor starlark.String = "file"

func newFile(key string, fi fs.FileInfo) (*starlarkstruct.Struct, error) {
	name := fi.Name()
	dir := path.Dir(key)
	ospath, err := filepath.Abs(filepath.FromSlash(key))
	if err != nil {
		return nil, err
	}
	return starlarkstruct.FromStringDict(fileConstructor, starlark.StringDict{
		"basename":     starlark.String(path.Base(name)),
		"dirname":      starlark.String(filepath.FromSlash(dir)),
		"extension":    starlark.String(path.Ext(name)),
		"path":         starlark.String(ospath),
		"is_directory": starlark.Bool(fi.IsDir()),
		//"is_source":    starlark.Bool(isSource),
		"size": starlark.MakeInt64(fi.Size()),
	}), nil
}

// target lazily resolves the action to a starlark value.
type target struct {
	label  string
	action *Action
}

func newTarget(label string, action *Action) *target {
	return &target{
		label:  label,
		action: action,
	}
}

func (t *target) String() string {
	return fmt.Sprintf("target(label = %s, value = %s)", t.label, t.action.Value)
}
func (t *target) Type() string          { return "target" }
func (t *target) Truth() starlark.Bool  { return t.action.Value.Truth() }
func (t *target) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable") }
func (t *target) Freeze()               {} // immutable

// Attr returns the value of the specified field.
func (t *target) Attr(name string) (starlark.Value, error) {
	switch name {
	case "label":
		return starlark.String(t.label), nil
	case "value":
		return t.action.Value, nil
	default:
		return nil, starlark.NoSuchAttrError(
			fmt.Sprintf("target has no .%s attribute", name))
	}
}

// AttrNames returns a new sorted list of the struct fields.
func (t *target) AttrNames() []string {
	return []string{"label", "value"}
}

/*// Label is a path to a rule or file.
type Label struct {
	Name string
	Dir  string
}

func (l *Label) String() string {
	buf := new(strings.Builder)
	buf.WriteString("label")
	buf.WriteByte('(')
	buf.WriteString("name = ")
	buf.WriteString(l.Name)
	buf.WriteString("dir = ")
	buf.WriteString(l.Dir)
	buf.WriteByte(')')
	return buf.String()
}
func (l *Label) Type() string         { return "label" }
func (l *Label) Truth() starlark.Bool { return true } // even when empty
func (l *Label) Hash() (uint32, error) {
	// Hash simplified from struct hash.
	var x uint32 = 8731
	for _, s := range []string{l.Name, l.Dir} {
		namehash, _ := starlark.String(s).Hash()
		x = x ^ 3*namehash
	}
	return x, nil
}
func (l *Label) Freeze() {} // immutable

func ParseLabel(label string) (Label, error)*/

func newCtxModule(ctx context.Context, key string, attrs starlark.StringDict) *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "ctx",
		Members: starlark.StringDict{
			"actions": newActionsModule(ctx, key),

			"os":   starlark.String(runtime.GOOS),
			"arch": starlark.String(runtime.GOARCH),

			// TODO: make better???
			"build_dir":       starlark.String(path.Dir(key)),
			"build_file_path": starlark.String(path.Join(path.Dir(key), "BUILD.star")),

			"key":   starlark.String(key),
			"attrs": starlarkstruct.FromStringDict(Attrs, attrs),
		},
	}
}

type actions struct {
	ctx context.Context
	key string
}

func newActionsModule(ctx context.Context, key string) *starlarkstruct.Module {
	a := &actions{ctx, key}
	return &starlarkstruct.Module{
		Name: "actions",
		Members: starlark.StringDict{
			"run":       starlark.NewBuiltin("actions.run", a.run),
			"file":      starlark.NewBuiltin("actions.file", a.file),
			"packaging": newPackagingModule(a),
			"container": newContainerModule(a),
		},
	}
}

func (a *actions) run(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name    string
		argList *starlark.List
		envList *starlark.List
	)

	if err := starlark.UnpackArgs(
		"run", args, kwargs,
		"name", &name, "args", &argList, "env?", &envList,
	); err != nil {
		return nil, err
	}

	var (
		x       starlark.Value
		cmdArgs []string
		cmdEnv  []string
	)

	iter := argList.Iterate()
	for iter.Next(&x) {
		s, ok := starlark.AsString(x)
		if !ok {
			return nil, fmt.Errorf("error: unexpected run arg: %v", x)
		}
		cmdArgs = append(cmdArgs, s)
	}
	iter.Done()

	iter = envList.Iterate()
	for iter.Next(&x) {
		s, ok := starlark.AsString(x)
		if !ok {
			return nil, fmt.Errorf("error: unexpected run env: %v", x)
		}
		cmdEnv = append(cmdEnv, s)
	}
	iter.Done()

	cmd := exec.CommandContext(a.ctx, name, cmdArgs...)
	// TODO: set dir via args?
	cmd.Dir = path.Dir(a.key)
	cmd.Env = append(os.Environ(), cmdEnv...)

	var output bytes.Buffer
	cmd.Stderr = &output
	cmd.Stdout = &output

	if err := cmd.Run(); err != nil {
		//os.RemoveAll(tmpDir)
		log.Printf("Unexpected error: %v\n%v", err, output.String())
		return nil, err
	}

	return starlark.None, nil
}

func (a *actions) file(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name string
	)

	if err := starlark.UnpackArgs(
		"file", args, kwargs,
		"name", &name,
	); err != nil {
		return nil, err
	}

	fi, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	return newFile(name, fi)

}

// rule a laze build rule for implementing actions.
type rule struct {
	builder *Builder
	module  string

	impl  *starlark.Function  // implementation function
	attrs map[string]*attr    // attribute types
	args  starlark.StringDict // attribute args

	frozen bool
}

func (r *rule) String() string       { return "rule()" }
func (r *rule) Type() string         { return "rule" }
func (r *rule) Freeze()              { r.frozen = true }
func (r *rule) Truth() starlark.Bool { return starlark.Bool(!r.frozen) }
func (r *rule) Hash() (uint32, error) {
	// TODO: can a rule be hashed?
	return 0, fmt.Errorf("unhashable type: rule")
}

// makeRule creates a new rule instance. Accepts the following optional kwargs:
// "implementation", "attrs".
//
func (b *Builder) rule(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		impl  = new(starlark.Function)
		attrs = new(starlark.Dict)
	)
	if err := starlark.UnpackArgs(
		"rule", args, kwargs,
		"impl", &impl, "attrs?", &attrs,
	); err != nil {
		return nil, err
	}

	// type checks
	if impl.NumParams() != 1 {
		return nil, fmt.Errorf("unexpected number of params: %d", impl.NumParams())
	}

	m := make(map[string]*attr)
	for _, item := range attrs.Items() {
		name := string(item[0].(starlark.String))
		a, ok := item[1].(*attr)
		if !ok {
			return nil, fmt.Errorf("unexpected attribute value type: %T", item[1])
		}
		m[name] = a
	}
	if _, ok := m["name"]; ok {
		return nil, fmt.Errorf("name cannot be an attribute")
	}
	m["name"] = &attr{
		typ:       attrTypeString,
		def:       starlark.String(""),
		doc:       "Name of rule",
		mandatory: true,
	}

	return &rule{
		builder: b,
		impl:    impl,
		attrs:   m, // key -> type
	}, nil
}

// genrule(
// 	cmd = "protoc ...",
// 	deps = ["//:label"],
// 	outs = ["//"],
// 	executable = "file",
// )

var isStringAlphabetic = regexp.MustCompile(`^[a-zA-Z0-9_.]*$`).MatchString

func (r *rule) Name() string { return "rule" }
func (r *rule) CallInternal(thread *starlark.Thread, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	// Call from go(...)
	if len(args) > 0 {
		return nil, fmt.Errorf("error: got %d arguments, want 0", len(args))
	}

	attrSeen := make(map[string]bool)
	attrArgs := make(starlark.StringDict)
	for _, kwarg := range kwargs {
		name := string(kwarg[0].(starlark.String))
		value := kwarg[1]
		//value.Freeze()? Needed?
		fmt.Println("\t\tname:", name)

		a, ok := r.attrs[name]
		if !ok {
			return nil, fmt.Errorf("unexpected attribute: %s", name)
		}

		// Type check attributes args.
		switch a.typ {
		case attrTypeBool:
			_, ok = value.(starlark.Bool)
		case attrTypeInt:
			_, ok = value.(starlark.Int)
		case attrTypeIntList:
			_, ok = value.(*starlark.List)
			// TODO: list check
		case attrTypeLabel:
			_, ok = value.(starlark.String)
		//case attrTypeLabelKeyedStringDict:
		case attrTypeLabelList:
			_, ok = value.(*starlark.List)
		case attrTypeOutput:
			_, ok = value.(starlark.String)
		case attrTypeOutputList:
			_, ok = value.(*starlark.List)
		case attrTypeString:
			_, ok = value.(starlark.String)
		//case attrTypeStringDict:
		case attrTypeStringList:
			_, ok = value.(*starlark.List)
		//case attrTypeStringListDict:

		default:
			panic(fmt.Sprintf("unhandled type: %s", a.typ))
		}
		if !ok {
			return nil, fmt.Errorf("invalid field %s(%s): %v", name, a.typ, value)
		}

		fmt.Println("setting value", name, value)
		attrArgs[name] = value
		attrSeen[name] = true
	}

	// Mandatory checks
	for name, a := range r.attrs {
		if !attrSeen[name] {
			if a.mandatory {
				return nil, fmt.Errorf("missing mandatory attribute: %s", name)
			}
			attrArgs[name] = a.def
		}
	}
	r.args = attrArgs

	module, ok := thread.Local("module").(string)
	if !ok {
		return nil, fmt.Errorf("error internal: unknown module")
	}

	// name has to exist.
	name := string(attrArgs["name"].(starlark.String))

	// TODO: name validation?
	if !isStringAlphabetic(name) {
		return nil, fmt.Errorf("error: invalid name: %s", name)
	}

	dir := path.Dir(module)
	key := path.Join(dir, name)

	// Register rule in the build.
	if _, ok := r.builder.rulesCache[key]; ok {
		return nil, fmt.Errorf("duplicate rule registered: %s", key)
	}
	if r.builder.rulesCache == nil {
		r.builder.rulesCache = make(map[string]*rule)
	}
	r.builder.rulesCache[key] = r

	return starlark.None, nil
}
