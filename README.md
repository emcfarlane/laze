# [WIP] laze

{fast,~~correct~~,simple} - Choose two

## Why laze?

Laze is a simple build tool with skylark configuration.
Similar to bazel it builds an action graph to exectute.
Unlike bazel it doesn't focus on the correctness of builds.
This is a tradeoff from the complexity that adds.
Instead it hands off the complexity to the tools.
This plays nicely with mordern tooling and avoids reinventing the compiliers
for each language.
However because of this it will always be slightly janky.
So use at your own risk.


# Install

```
go get install github.com/emcfarlane/laze/cmd/laze
```

# Docs 

## Labels

Labels are what laze uses to identify resources. 
Under the hood labels are represented as URLs.
Relative paths are accepted too.

- `path/to/file.txt` : Relative path to file from the directory.
- `../sibling/file.txt` : Relative path to folder in parent directory.
- `file://path/from/root" : Local path from command root.
- `file:///usr/bin/cat" : Absolute path in local filesystem.
- `https://remote.com/source.py` : Remote file over http.

###  Label Query Parameters

Label targets can take query parameters to override target fields.

```
go(
    name = "binary",
)

container_image(
  name="app",
  layer=[
    "binary?os=linux&arch=amd64"
  ],
)
```

For instance with container images the binaries will always want to be targeted 
to the architecture of the containers runtime (usually linux).
But on the host we will want to execute the binaries under the host arch.
Therefore we can use the host as the default and override to the platform with
query parameters. Avoiding the need to specify build flags on every invocation.


### Label Protocols

Supported protocols:
- `https://`
TODO(edward): add dynamic support for protocols.


## Builtins

### go

Go builds!

```
go(
  name = "mycmd"
)
```

[Example](testdata/go/BUILD.star)

#### cgo

CGO is support through `zig`!

[Example](testdata/cgo/BUILD.star)

### container

Containers are supported with [github.com/google/go-containerregistry](github.com/google/go-containerregistry)

[Example](testdata/container/BUILD.star)

### proto

Protobuffers are supported with native `protoc`.

### TODO

If you have a usecase for laze and would like support adding please file an issue!
