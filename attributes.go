package laze

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

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
	def        starlark.Value // default
	doc        string
	executable bool
	mandatory  bool
	allowEmpty bool
	allowFiles allowedFiles // nil, bool, globlist([]string)
	values     interface{}  // []typ
}

func (a *attr) String() string {
	var b strings.Builder
	b.WriteString(string(a.typ))
	b.WriteString("(")
	b.WriteString("default = ")
	b.WriteString(a.def.String())
	b.WriteString(", doc = " + a.doc)
	b.WriteString(", executable = ")
	b.WriteString(starlark.Bool(a.executable).String())
	b.WriteString(", mandatory = ")
	b.WriteString(starlark.Bool(a.mandatory).String())
	b.WriteString(", allow_empty = ")
	b.WriteString(starlark.Bool(a.allowEmpty).String())
	b.WriteString(", allow_files = ")
	b.WriteString(starlark.Bool(a.allowFiles.allow).String())

	b.WriteString(", values = ")
	switch v := a.values.(type) {
	case []string, []int:
		b.WriteString(fmt.Sprintf("%v", v))
	case nil:
		b.WriteString(starlark.None.String())
	default:
		panic(fmt.Sprintf("unhandled values type: %T", a.values))
	}
	b.WriteString(")")
	return b.String()
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

type allowedFiles struct {
	allow bool
	types []string
}

func parseAllowFiles(allowFiles starlark.Value) (allowedFiles, error) {
	switch v := allowFiles.(type) {
	case nil:
		return allowedFiles{allow: false}, nil
	case starlark.Bool:
		return allowedFiles{allow: bool(v)}, nil
	default:
		panic(fmt.Sprintf("TODO: handle allow_files type: %T", allowFiles))
	}
}

// Attribute attr.label(default=None, doc='', executable=False, allow_files=None, allow_single_file=None, mandatory=False, providers=[], allow_rules=None, cfg=None, aspects=[])
func attrLabel(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		def        starlark.String
		doc        string
		executable = false
		mandatory  bool
		values     *starlark.List
		allowFiles starlark.Value

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

	af, err := parseAllowFiles(allowFiles)
	if err != nil {
		return nil, err
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
		allowEmpty bool = true
		allowFiles starlark.Value
	)
	if err := starlark.UnpackArgs(
		"attr.bool", args, kwargs,
		"default?", &def, "doc?", &doc, "mandatory?", &mandatory, "allow_empty?", &allowEmpty, "allow_files?", &allowFiles,
	); err != nil {
		return nil, err
	}

	// TODO: default checks?
	if def != nil {
		iter := def.Iterate()
		var x starlark.Value
		for iter.Next(&x) {
			if _, ok := starlark.AsString(x); !ok {
				return nil, fmt.Errorf("got %s, want string", x.Type())
			}
		}
		iter.Done()
	}

	af, err := parseAllowFiles(allowFiles)
	if err != nil {
		return nil, err
	}

	return &attr{
		typ:        attrTypeLabelList,
		def:        def,
		doc:        doc,
		mandatory:  mandatory,
		allowEmpty: allowEmpty,
		allowFiles: af,
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

	// Check defaults are all strings
	if def != nil {
		iter := def.Iterate()
		var x starlark.Value
		for iter.Next(&x) {
			if _, ok := starlark.AsString(x); !ok {
				return nil, fmt.Errorf("got %s, want string", x.Type())
			}
		}
		iter.Done()
	}

	return &attr{
		typ:       attrTypeStringList,
		def:       def,
		doc:       doc,
		mandatory: mandatory,
	}, nil
}
