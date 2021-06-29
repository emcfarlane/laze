package laze

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

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
			"run": starlark.NewBuiltin("actions.run", a.run),
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

var isStringAlphabetic = regexp.MustCompile(`^[a-zA-Z0-9_]*$`).MatchString

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

func newAttrModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "attr",
		Members: starlark.StringDict{
			"bool":     starlark.NewBuiltin("attr.bool", attrBool),
			"int":      starlark.NewBuiltin("attr.int", attrInt),
			"int_list": starlark.NewBuiltin("attr.int_list", attrIntList),
			"label":    starlark.NewBuiltin("attr.label", attrLabel),
			//TODO:"label_keyed_string_dict": starlark.NewBuiltin("attr.label_keyed_string_dict", attrLabelKeyedStringDict),
			"label_list":  starlark.NewBuiltin("attr.label_list", attrLabelList),
			"output":      starlark.NewBuiltin("attr.output", attrOutput),
			"output_list": starlark.NewBuiltin("attr.output_list", attrOutputList),
			"string":      starlark.NewBuiltin("attr.string", attrString),
			//TODO:"string_dict":             starlark.NewBuiltin("attr.string_dict", attrStringDict),
			"string_list": starlark.NewBuiltin("attr.string_list", attrStringList),
			//TODO:"string_list_dict":        starlark.NewBuiltin("attr.string_list_dict", attrStringListDict),
		},
	}
}

type attrType string

const (
	attrTypeBool                 attrType = "attr.bool"
	attrTypeInt                           = "attr.int"
	attrTypeIntList                       = "attr.int_list"
	attrTypeLabel                         = "attr.label"
	attrTypeLabelKeyedStringDict          = "attr.label_keyed_string_dict"
	attrTypeLabelList                     = "attr.label_list"
	attrTypeOutput                        = "attr.output"
	attrTypeOutputList                    = "attr.output_list"
	attrTypeString                        = "attr.string"
	attrTypeStringDict                    = "attr.string_dict"
	attrTypeStringList                    = "attr.string_list"
	attrTypeStringListDict                = "attr.string_list_dict"

	Attrs starlark.String = "attrs" // starlarkstruct constructor
)

// attr defines attributes to a rules attributes.
type attr struct {
	typ        attrType
	def        starlark.Value
	doc        string
	executable bool
	mandatory  bool
	allowEmpty bool
	allowFiles interface{} // nil, bool, globlist([]string)
	values     interface{} // []typ
}

func (a *attr) String() string {
	var s string
	s += "default = " + a.def.String()
	s += ", doc = " + a.doc
	s += ", executable = " + starlark.Bool(a.executable).String()
	s += ", mandatory = " + starlark.Bool(a.mandatory).String()
	s += ", allow_empty = " + starlark.Bool(a.allowEmpty).String()
	s += ", allow_files = "
	switch v := a.allowFiles.(type) {
	case []string:
		s += fmt.Sprintf("%v", v)
	case bool:
		s += starlark.Bool(v).String()
	case nil:
		s += starlark.None.String()
	default:
		panic(fmt.Sprintf("unhandled allow_files type: %T", a.allowFiles))
	}

	s += ", values = "
	switch v := a.values.(type) {
	case []string, []int:
		s += fmt.Sprintf("%v", v)
	case nil:
		s += starlark.None.String()
	default:
		panic(fmt.Sprintf("unhandled values type: %T", a.values))
	}

	return fmt.Sprintf("%s(%s)", a.typ, s)
}
func (a *attr) Type() string         { return string(a.typ) }
func (a *attr) Freeze()              {} // immutable
func (a *attr) Truth() starlark.Bool { return true }
func (a *attr) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", a.Type())
}

// Attribute attr.bool(default=False, doc='', mandatory=False)
func attrBool(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def       bool
		doc       string
		mandatory bool
	)

	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "mandatory?", &mandatory,
	); err != nil {
		return nil, err
	}

	return &attr{
		typ:       attrTypeBool,
		def:       starlark.Bool(def),
		doc:       doc,
		mandatory: mandatory,
	}, nil
}

// Attribute attr.int(default=0, doc='', mandatory=False, values=[])
func attrInt(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def       = starlark.MakeInt(0)
		doc       string
		mandatory bool
		values    *starlark.List
	)

	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "mandatory?", &mandatory, "values?", &values,
	); err != nil {
		return nil, err
	}

	var ints []int
	if values != nil {
		iter := values.Iterate()
		var x starlark.Value
		for iter.Next(&x) {
			i, err := starlark.AsInt32(x)
			if err != nil {
				return nil, err
			}
			ints = append(ints, i)
		}
		iter.Done()
	}

	return &attr{
		typ:       attrTypeInt,
		def:       def,
		doc:       doc,
		mandatory: mandatory,
		values:    ints,
	}, nil
}

// Attribute attr.int_list(mandatory=False, allow_empty=True, *, default=[], doc='')
func attrIntList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def        *starlark.List
		doc        string
		mandatory  bool
		allowEmpty bool
	)
	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "mandatory?", &mandatory, "allowEmpty?", &allowEmpty,
	); err != nil {
		return nil, err
	}

	iter := def.Iterate()
	var x starlark.Value
	for iter.Next(&x) {
		if _, err := starlark.AsInt32(x); err != nil {
			return nil, err
		}
	}
	iter.Done()

	return &attr{
		typ:        attrTypeIntList,
		def:        def,
		doc:        doc,
		mandatory:  mandatory,
		allowEmpty: allowEmpty,
	}, nil
}

// Attribute attr.label(default=None, doc='', executable=False, allow_files=None, allow_single_file=None, mandatory=False, providers=[], allow_rules=None, cfg=None, aspects=[])
func attrLabel(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def        starlark.String
		doc        string
		executable = false
		mandatory  bool
		values     *starlark.List
		allowFiles interface{}

		// TODO: more types!
		//providers
	)

	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "executable", &executable, "mandatory?", &mandatory, "values?", &values, "allow_files?", &allowFiles,
	); err != nil {
		return nil, err
	}

	var vals []string
	if values != nil {
		iter := values.Iterate()
		var x starlark.Value
		for iter.Next(&x) {
			s, ok := starlark.AsString(x)
			if !ok {
				return nil, fmt.Errorf("got %s, want string", x.Type())
			}
			vals = append(vals, s)
		}
		iter.Done()
	}

	var af interface{}
	switch v := allowFiles.(type) {
	case starlark.Bool:
		af = bool(v)
	default:
		panic(fmt.Sprintf("TODO: handle allow_files type: %T", allowFiles))
	}

	return &attr{
		typ:        attrTypeLabel,
		def:        def,
		doc:        doc,
		mandatory:  mandatory,
		values:     vals,
		allowFiles: af,
	}, nil
}

// attr.label_list(allow_empty=True, *, default=[], doc='', allow_files=None, providers=[], flags=[], mandatory=False, cfg=None, aspects=[])
func attrLabelList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def        *starlark.List
		doc        string
		mandatory  bool
		allowEmpty = true
		allowFiles starlark.Value
	)
	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "mandatory?", &mandatory, "allow_empty?", &allowEmpty, "allow_files?", &allowFiles,
	); err != nil {
		return nil, err
	}

	iter := def.Iterate()
	var x starlark.Value
	for iter.Next(&x) {
		if _, ok := starlark.AsString(x); !ok {
			return nil, fmt.Errorf("got %s, want string", x.Type())
		}
	}
	iter.Done()

	return &attr{
		typ:        attrTypeLabelList,
		def:        def,
		doc:        doc,
		mandatory:  mandatory,
		allowEmpty: allowEmpty,
		allowFiles: allowFiles,
	}, nil
}

// Attribute attr.output(doc='', mandatory=False)
func attrOutput(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		doc       string
		mandatory bool
	)

	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"doc?", &doc, "mandatory?", &mandatory,
	); err != nil {
		return nil, err
	}

	return &attr{
		typ:       attrTypeOutput,
		doc:       doc,
		mandatory: mandatory,
	}, nil
}

// Attribute attr.output_list(allow_empty=True, *, doc='', mandatory=False)
func attrOutputList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		doc        string
		mandatory  bool
		allowEmpty bool
	)
	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"doc?", &doc, "mandatory?", &mandatory, "allowEmpty?", &allowEmpty,
	); err != nil {
		return nil, err
	}

	return &attr{
		typ:        attrTypeOutputList,
		doc:        doc,
		mandatory:  mandatory,
		allowEmpty: allowEmpty,
	}, nil
}

func attrString(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def       starlark.String
		doc       string
		mandatory bool
		values    *starlark.List
	)

	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "mandatory?", &mandatory, "values?", &values,
	); err != nil {
		return nil, err
	}

	var strings []string
	if values != nil {
		iter := values.Iterate()
		var x starlark.Value
		for iter.Next(&x) {
			s, ok := starlark.AsString(x)
			if !ok {
				return nil, fmt.Errorf("got %s, want string", x.Type())
			}
			strings = append(strings, s)
		}
		iter.Done()
	}

	return &attr{
		typ:       attrTypeString,
		def:       def,
		doc:       doc,
		mandatory: mandatory,
		values:    strings,
	}, nil
}

func attrStringList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def        *starlark.List
		doc        string
		mandatory  bool
		allowEmpty bool
	)
	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "mandatory?", &mandatory, "allowEmpty?", &allowEmpty,
	); err != nil {
		return nil, err
	}

	iter := def.Iterate()
	var x starlark.Value
	for iter.Next(&x) {
		if _, ok := starlark.AsString(x); !ok {
			return nil, fmt.Errorf("got %s, want string", x.Type())
		}
	}
	iter.Done()

	return &attr{
		typ:       attrTypeStringList,
		def:       def,
		doc:       doc,
		mandatory: mandatory,
	}, nil
}
