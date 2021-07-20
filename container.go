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
			"pull":  starlark.NewBuiltin("container.run", c.pull),
			"build": starlark.NewBuiltin("container.run", c.build),
			"push":  starlark.NewBuiltin("container.run", c.push),
		},
	}
}

const imageConstructor starlark.String = "image"

// TODO: return starlark provider.
func newImage(filename, tag string) starlark.Value {
	return starlarkstruct.FromStringDict(imageConstructor, map[string]starlark.Value{
		"name": starlark.String(filename),
		"tag":  starlark.String(tag),
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

	// TODO: caching...
	ref, err := name.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, err
	}

	// TODO: how the fuck do files work?
	filename := "image_base.tar"
	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// TODO: tag? what is the tag value???
	tag := "latest"

	if err := tarball.Write(ref, img, f); err != nil {
		return nil, err
	}
	return newImage(filename, tag), nil
}

func (c *container) build(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name              string
		entrypoint        []string
		tar               *target
		base              *target
		prioritized_files []string
	)
	if err := starlark.UnpackArgs(
		"container_build", args, kwargs,
		"name", &name,
		"entrypoint", &entrypoint,
		"tar", &tar,
		"base?", &base,
		"prioritized_files?", &prioritized_files,
	); err != nil {
		return nil, err
	}

	// TODO: load tag from provider?

	baseImage := empty.Image
	if base != nil {
		// Load base iamge from local.
		imageProvider, err := base.action.loadStructValue(imageConstructor)
		if err != nil {
			return nil, fmt.Errorf("image provider: %w", err)
		}
		filename, err := imageProvider.AttrString("name")
		if err != nil {
			return nil, err
		}

		tagStr, err := imageProvider.AttrString("tag")
		if err != nil {
			return nil, err
		}

		tag, err := cname.NewTag(tagStr, cname.StrictValidation)
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

	// Construct a tarball with the binary and produce a layer.
	//binaryLayerBuf, err := tarBinary(appPath, file, v1.Time{})
	//if err != nil {
	//	return nil, err
	//}
	//binaryLayerBytes := binaryLayerBuf.Bytes()

	tarVal, ok := tar.action.Value.(*starlarkstruct.Struct)
	if !ok {
		return nil, fmt.Errorf("invalid tar type")
	}
	tarFileVal, err := tarVal.Attr("file")
	if err != nil {
		return nil, err
	}

	tarFileStruct, ok := tarFileVal.(*starlarkstruct.Struct)
	if !ok {
		return nil, fmt.Errorf("malformed tar file provider: invalid tar type")
	}

	tarFilenameVal, err := tarFileStruct.Attr("name")
	if err != nil {
		return nil, err
	}
	tarFilename := string(tarFilenameVal.(starlark.String))

	r, err := os.Open(tarFilename)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	imageLayer, err := tarball.LayerFromReader(
		r, tarball.WithCompressedCaching,
		tarball.WithEstargzOptions(estargz.WithPrioritizedFiles(
			// When using estargz, prioritize downloading the binary entrypoint.
			prioritized_files,
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
			//Comment:   "go build output, at " + appPath,
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

	filename := "image.tar"
	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	reference := "gcr.io/foo/bar"
	ref, err := cname.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	if err := tarball.Write(ref, img, f); err != nil {
		return nil, err
	}

	tag := "latest" // TODO: tag?
	return newImage(filename, tag), nil
}

func (c *container) push(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name              string
		entrypoint        []string
		tar               *target
		base              *target
		prioritized_files []string
	)
	if err := starlark.UnpackArgs(
		"container_push", args, kwargs,
		"name", &name,
		"entrypoint", &entrypoint,
		"tar", &tar,
		"base?", &base,
		"prioritized_files?", &prioritized_files,
	); err != nil {
		return nil, err
	}

	imgpath := "image"

	tag, err := cname.NewTag("gcr.io/foo/bar:latest", cname.StrictValidation)
	if err != nil {
		return nil, err
	}

	// Load base from filesystem.
	img, err := tarball.ImageFromPath(imgpath, &tag)
	if err != nil {
		return nil, err
	}

	reference := "gcr.io/foo/bar"
	ref, err := cname.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	if err := remote.Write(ref, img); err != nil {
		return nil, err
	}

	return nil, nil
}

/*func (b *Builder) buildOne(ctx context.Context, s string, base v1.Image, platform *v1.Platform) (v1.Image, error) {
	ref := newRef(s)

	cf, err := base.ConfigFile()
	if err != nil {
		return nil, err
	}
	if platform == nil {
		platform = &v1.Platform{
			OS:           cf.OS,
			Architecture: cf.Architecture,
			OSVersion:    cf.OSVersion,
		}
	}

	// Do the build into a temporary file.
	file, err := g.build(ctx, ref.Path(), g.dir, *platform, g.configForImportPath(ref.Path()))
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(filepath.Dir(file))

	var layers []mutate.Addendum
	// Create a layer from the kodata directory under this import path.
	dataLayerBuf, err := g.tarKoData(ref)
	if err != nil {
		return nil, err
	}
	dataLayerBytes := dataLayerBuf.Bytes()
	dataLayer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewBuffer(dataLayerBytes)), nil
	}, tarball.WithCompressedCaching)
	if err != nil {
		return nil, err
	}
	layers = append(layers, mutate.Addendum{
		Layer: dataLayer,
		History: v1.History{
			Author:    "ko",
			CreatedBy: "ko publish " + ref.String(),
			Comment:   "kodata contents, at $KO_DATA_PATH",
		},
	})

	appPath := path.Join(appDir, appFilename(ref.Path()))

	// Construct a tarball with the binary and produce a layer.
	binaryLayerBuf, err := tarBinary(appPath, file, v1.Time{})
	if err != nil {
		return nil, err
	}
	binaryLayerBytes := binaryLayerBuf.Bytes()
	binaryLayer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewBuffer(binaryLayerBytes)), nil
	}, tarball.WithCompressedCaching, tarball.WithEstargzOptions(estargz.WithPrioritizedFiles([]string{
		// When using estargz, prioritize downloading the binary entrypoint.
		appPath,
	})))
	if err != nil {
		return nil, err
	}
	layers = append(layers, mutate.Addendum{
		Layer: binaryLayer,
		History: v1.History{
			Author:    "ko",
			CreatedBy: "ko publish " + ref.String(),
			Comment:   "go build output, at " + appPath,
		},
	})

	// Augment the base image with our application layer.
	withApp, err := mutate.Append(base, layers...)
	if err != nil {
		return nil, err
	}

	// Start from a copy of the base image's config file, and set
	// the entrypoint to our app.
	cfg, err := withApp.ConfigFile()
	if err != nil {
		return nil, err
	}

	//cfg = cfg.DeepCopy()
	//cfg.Config.Entrypoint = []string{appPath}
	//updatePath(cfg)
	//cfg.Config.Env = append(cfg.Config.Env, "KO_DATA_PATH="+kodataRoot)
	//cfg.Author = "github.com/emcfarlane/laze"

	if cfg.Config.Labels == nil {
		cfg.Config.Labels = map[string]string{}
	}
	for k, v := range g.labels {
		cfg.Config.Labels[k] = v
	}

	image, err := mutate.ConfigFile(withApp, cfg)
	if err != nil {
		return nil, err
	}

	empty := v1.Time{}
	if g.creationTime != empty {
		return mutate.CreatedAt(image, g.creationTime)
	}
	return image, nil
}*/
