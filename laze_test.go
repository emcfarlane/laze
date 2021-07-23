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

	b := Builder{
		Dir: "", // TODO: testdata dir?
	}

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
	}, {
		name:            "containerPull",
		label:           "testdata/container/distroless.tar",
		wantConstructor: imageConstructor,
	}, {
		name:            "containerBuild",
		label:           "testdata/container/helloc.tar",
		wantConstructor: imageConstructor,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := b.Build(ctx, nil, tt.label)
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

func TestLabels(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		dir     string
		want    string
		wantErr error
	}{{
		name:  "full",
		label: "testdata/go/hello",
		dir:   ".",
		want:  "file://testdata/go/hello",
	}, {
		name:  "full2",
		label: "testdata/go/hello",
		dir:   "",
		want:  "file://testdata/go/hello",
	}, {
		name:  "relative",
		label: "hello",
		dir:   "testdata/go",
		want:  "file://testdata/go/hello",
	}, {
		name:  "dotRelative",
		label: "./hello",
		dir:   "testdata/go",
		want:  "file://testdata/go/hello",
	}, {
		name:  "dotdotRelative",
		label: "../hello",
		dir:   "testdata/go",
		want:  "file://testdata/hello",
	}, {
		name:  "dotdotdotdotRelative",
		label: "../../hello",
		dir:   "testdata/go",
		want:  "file://hello",
	}, {
		name:  "dotdotCD",
		label: "../packaging/file",
		dir:   "testdata/go",
		want:  "file://testdata/packaging/file",
	}, {
		name:  "absolute",
		label: "/users/edward/Downloads/file.txt",
		dir:   "",
		want:  "file:///users/edward/Downloads/file.txt",
	}, {
		name:  "fileLabel",
		label: "file://rules/go/zxx",
		dir:   "testdata/cgo",
		want:  "file://rules/go/zxx",
	}, {
		name:  "queryRelative",
		label: "helloc?goarch=amd64&goos=linux",
		dir:   "testdata/cgo",
		want:  "file://testdata/cgo/helloc?goarch=amd64&goos=linux",
	}, {
		name:  "queryAbsolute",
		label: "file://testdata/cgo/helloc?goarch=amd64&goos=linux",
		dir:   "testdata/cgo",
		want:  "file://testdata/cgo/helloc?goarch=amd64&goos=linux",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := parseLabel(tt.label, tt.dir)
			if err != tt.wantErr {
				t.Fatalf("error got: %v, want: %v", err, tt.wantErr)
			}
			if u.String() != tt.want {
				t.Fatalf("%s != %s", u, tt.want)
			}
		})
	}
}
