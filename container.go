package laze

import (
	"fmt"
	"os"

	"github.com/containerd/stargz-snapshotter/estargz"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	cname "github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// conatiner rules implemented with go-containerregistry.
// Based on:
// https://github.com/google/ko/blob/main/pkg/build/gobuild.go
// https://github.com/bazelbuild/rules_docker/tree/master/container
type container struct {
	*actions
}

func newContainerModule(a *actions) *starlarkstruct.Module {
	c := container{a}
	return &starlarkstruct.Module{
		Name: "container",
		Members: starlark.StringDict{
			"pull":  starlark.NewBuiltin("container.pull", c.pull),
			"build": starlark.NewBuiltin("container.build", c.build),
			"push":  starlark.NewBuiltin("container.push", c.push),
		},
	}
}

const imageConstructor starlark.String = "image"

// TODO: return starlark provider.
func newImage(filename, reference string) starlark.Value {
	return starlarkstruct.FromStringDict(imageConstructor, map[string]starlark.Value{
		"name":      starlark.String(filename),
		"reference": starlark.String(reference),
	})
}

func (c *container) pull(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		rname     string
		reference string
	)
	if err := starlark.UnpackArgs(
		"container_pull", args, kwargs,
		"name", &rname, "reference", &reference,
	); err != nil {
		return nil, err
	}

	ref, err := name.ParseReference(reference)
	if err != nil {
		return nil, err
	}
	ref.Context()

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, err
	}

	// TODO: caching...
	// HACK: lets just stat the existance of the file
	filename := c.key
	if _, err := os.Stat(filename); err != nil {
		f, err := os.Create(filename)
		if err != nil {
			panic(err)
		}
		defer f.Close()

		if err := tarball.Write(ref, img, f); err != nil {
			return nil, err
		}
	}
	return newImage(filename, reference), nil
}

func listToStrings(l *starlark.List) ([]string, error) {
	iter := l.Iterate()
	defer iter.Done()

	var ss []string

	var x starlark.Value
	for iter.Next(&x) {
		s, ok := starlark.AsString(x)
		if !ok {
			return nil, fmt.Errorf("invalid string list")
		}
		ss = append(ss, s)
	}
	return ss, nil
}

func (c *container) build(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name            string
		entrypointList  *starlark.List
		tar             *target
		base            *target
		prioritizedList *starlark.List
	)
	if err := starlark.UnpackArgs(
		"container_build", args, kwargs,
		"name", &name,
		"entrypoint", &entrypointList,
		"tar", &tar,
		"base?", &base,
		"prioritized_files?", &prioritizedList,
	); err != nil {
		return nil, err
	}

	// TODO: load tag from provider?
	entrypoint, err := listToStrings(entrypointList)
	if err != nil {
		return nil, err
	}
	prioritizedFiles, err := listToStrings(prioritizedList)
	if err != nil {
		return nil, err
	}

	baseImage := empty.Image
	if base != nil {
		// Load base iamge from local.
		imageProvider, err := base.action.loadStructValue(imageConstructor)
		if err != nil {
			return nil, fmt.Errorf("image provider: %w", err)
		}

		// TODO: should it be a file provider?
		filename, err := imageProvider.AttrString("name")
		if err != nil {
			return nil, err
		}

		reference, err := imageProvider.AttrString("reference")
		if err != nil {
			return nil, err
		}

		tag, err := cname.NewTag(reference, cname.StrictValidation)
		if err != nil {
			return nil, err
		}

		// Load base from filesystem.
		img, err := tarball.ImageFromPath(filename, &tag)
		if err != nil {
			return nil, err
		}
		baseImage = img
	}

	var layers []mutate.Addendum

	tarStruct, err := tar.action.loadStructValue(fileConstructor)
	if err != nil {
		return nil, err
	}
	tarFilename, err := tarStruct.AttrString("path")
	if err != nil {
		return nil, err
	}

	r, err := os.Open(tarFilename)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	imageLayer, err := tarball.LayerFromReader(
		r, tarball.WithCompressedCaching,
		tarball.WithEstargzOptions(estargz.WithPrioritizedFiles(
			// When using estargz, prioritize downloading the binary entrypoint.
			prioritizedFiles,
		)),
	)
	if err != nil {
		return nil, err
	}
	layers = append(layers, mutate.Addendum{
		Layer: imageLayer,
		History: v1.History{
			Author:    "laze",
			CreatedBy: "laze " + name,
			Comment:   "ship it real good",
		},
	})

	// Augment the base image with our application layer.
	appImage, err := mutate.Append(baseImage, layers...)
	if err != nil {
		return nil, err
	}

	// Start from a copy of the base image's config file, and set
	// the entrypoint to our app.
	cfg, err := appImage.ConfigFile()
	if err != nil {
		return nil, err
	}
	cfg = cfg.DeepCopy()
	cfg.Config.Entrypoint = entrypoint
	//updatePath(cfg)
	cfg.Config.Env = append(cfg.Config.Env, "LAZE_DATA_PATH="+"/") // TODO
	cfg.Author = "github.com/emcfarlane/laze"

	if cfg.Config.Labels == nil {
		cfg.Config.Labels = map[string]string{}
	}
	// TODO: Add support for labels.
	//for k, v := range labels {
	//	cfg.Config.Labels[k] = v
	//}

	img, err := mutate.ConfigFile(appImage, cfg)
	if err != nil {
		return nil, err
	}

	//empty := v1.Time{}
	//if g.creationTime != empty {
	//	return mutate.CreatedAt(image, g.creationTime)
	//}

	filename := c.key
	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	reference := "gcr.io/foo/bar:latest" // TODO: Reference?
	ref, err := cname.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	if err := tarball.Write(ref, img, f); err != nil {
		return nil, err
	}
	return newImage(filename, reference), nil
}

func (c *container) push(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name      string
		image     *target
		reference string
	)
	if err := starlark.UnpackArgs(
		"container_push", args, kwargs,
		"name", &name,
		"image", &image,
		"reference", &reference,
	); err != nil {
		return nil, err
	}

	tag, err := cname.NewTag(reference, cname.StrictValidation)
	if err != nil {
		return nil, err
	}

	imageProvider, err := image.action.loadStructValue(imageConstructor)
	if err != nil {
		return nil, fmt.Errorf("image provider: %w", err)
	}

	// TODO: should it be a file provider?
	filename, err := imageProvider.AttrString("name")
	if err != nil {
		return nil, err
	}

	// Load base from filesystem.
	img, err := tarball.ImageFromPath(filename, &tag)
	if err != nil {
		return nil, err
	}

	ref, err := cname.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	if err := remote.Write(ref, img); err != nil {
		return nil, err
	}
	return nil, nil
}
