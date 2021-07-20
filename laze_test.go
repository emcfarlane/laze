package laze

import (
	"context"
	"testing"

	"go.starlark.net/starlark"
)

//func (b *Builder) testFile(
//	basename string,
//	dirname string,
//	extension string,
//	filename string,
//	isDir bool,
//	size int64,
//) starlark.Value {
//	return starlarkstruct.FromStringDict(fileConstructor, starlark.StringDict{
//		"basename":     starlark.String(basename),
//		"dirname":      starlark.String(dirname),
//		"extension":    starlark.String(extension),
//		"filename":     starlark.String(filename),
//		"is_directory": starlark.Bool(isDir),
//		"size":         starlark.MakeInt64(size),
//	})
//}

func TestBuild(t *testing.T) {

	type result struct {
		value starlark.Value
		error error
	}

	b := Builder{}

	tests := []struct {
		name            string
		label           string
		wantConstructor starlark.Value
		wantErr         error
	}{{
		name:            "go",
		label:           "testdata/go/hello",
		wantConstructor: fileConstructor,
	}, {
		name:            "cgo",
		label:           "testdata/cgo/helloc",
		wantConstructor: fileConstructor,
	}, {
		name:            "xcgo",
		label:           "testdata/cgo/helloc?goarch=amd64&goos=linux",
		wantConstructor: fileConstructor,
	}, {
		name:            "tarxcgo",
		label:           "testdata/packaging/helloc.tar.gz",
		wantConstructor: fileConstructor,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := b.Build(ctx, tt.label)
			if err != nil {
				t.Fatal(err)
			}
			if got.Failed && got.Error != tt.wantErr {
				t.Fatalf("error got: %v, want: %v", got.Error, tt.wantErr)
			}
			if got.Failed {
				t.Fatal("error failed: ", got)
			}

			if c := tt.wantConstructor; c != nil {
				_, err := got.loadStructValue(c)
				if err != nil {
					t.Fatalf("error value: %v", err)
				}
			}
		})
	}

}
