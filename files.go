package laze

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

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

type files struct {
	*actions
}

func newFilesModule(a *actions) *starlarkstruct.Module {
	f := files{a}
	return &starlarkstruct.Module{
		Name: "files",
		Members: starlark.StringDict{
			"stat":    starlark.NewBuiltin("files.stat", f.stat),
			"write":   starlark.NewBuiltin("files.write", f.write),
			"declare": starlark.NewBuiltin("files.declare", f.declare),
		},
	}
}

func (f *files) stat(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name string
	)

	if err := starlark.UnpackArgs(
		"files.stat", args, kwargs,
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

func (f *files) write(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name    string
		content string
		mode    int
	)

	if err := starlark.UnpackArgs(
		"files.write", args, kwargs,
		"name", &name, "content", &content, "mode", &mode,
	); err != nil {
		return nil, err
	}

	if err := os.WriteFile(name, []byte(content), os.FileMode(mode)); err != nil {
		return nil, err
	}
	fi, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	return newFile(name, fi)

}

// declare
func (f *files) declare(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var elems []string
	for _, arg := range args {
		s, ok := starlark.AsString(arg)
		if !ok {
			return nil, fmt.Errorf("unexpected arg type: %s", arg)
		}
		elems = append(elems, s)
	}
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("unexpected kwargs")
	}
	name, err := filepath.Abs(filepath.FromSlash(path.Join(elems...)))
	if err != nil {
		return nil, err
	}
	dir := path.Dir(name)

	// Create the directory structure if it doesn't exist.
	if err := os.MkdirAll(dir, 0777); err != nil {
		return nil, err
	}
	return starlark.String(name), nil
}
