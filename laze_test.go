package laze

import (
	"context"
	"testing"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func TestBuild(t *testing.T) {

	type result struct {
		value starlark.Value
		error error
	}

	b := Builder{}

	tests := []struct {
		name    string
		label   string
		wants   []interface{}
		wantErr error
	}{{
		name:  "go",
		label: "testdata/go/hello",
		wants: []interface{}{
			result{
				value: starlarkstruct.FromStringDict(
					starlarkstruct.Default, starlark.StringDict{
						"outs": starlark.NewList([]starlark.Value{
							starlark.String("testdata/go/hello"),
						}),
					},
				),
			},
		},
	}, {
		name:  "cgo",
		label: "testdata/cgo/helloc",
		wants: []interface{}{
			result{
				value: starlarkstruct.FromStringDict(
					starlarkstruct.Default, starlark.StringDict{
						"outs": starlark.NewList([]starlark.Value{
							starlark.String("testdata/cgo/helloc"),
						}),
					},
				),
			},
		},
	}, {
		name:  "xcgo",
		label: "testdata/cgo/helloc?goarch=amd64&goos=linux",
		wants: []interface{}{
			result{
				value: starlarkstruct.FromStringDict(
					starlarkstruct.Default, starlark.StringDict{
						"outs": starlark.NewList([]starlark.Value{
							starlark.String("testdata/cgo/helloc"),
						}),
					},
				),
			},
		},
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

			for _, want := range tt.wants {
				switch v := want.(type) {
				case result:
					ok, err := starlark.Equal(got.Value, v.value)
					if err != nil {
						t.Error(err)
					}
					if !ok {
						t.Fatalf("got %v != %v", v.value, got.Value)
					}
				}
			}
		})
	}

}
