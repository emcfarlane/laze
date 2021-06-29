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

## Install with `laze` (TODO):

```
git(
  name="laze"
  src="https://github.com/emcfarlane/laze.git"
)
```

```
laze build laze/cmd/laze
```

# Docs 

## Labels

Labels are URLs:

- `path/to/file`
- `path/to/name`
- 'https://example.com/path/to/external`

###  Label Query Parameters

Label targets can take query parameters to override target fields.

```
container_image(
  name="app",
  layer=[
    "path/to/binary?os=linux&arch=amd64"
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
  name="mycmd"
)
```

#### cgo

CGO is support through `zig`!

### container

Containers are supported with [github.com/google/go-containerregistry](github.com/google/go-containerregistry)

Link 

### proto

Protobuffers are supported with native `protoc`.
