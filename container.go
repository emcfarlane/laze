package laze

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"

	"github.com/containerd/stargz-snapshotter/estargz"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"go.starlark.net/starlark"
)

// conatiner rules implemented with go-containerregistry.
// Based on:
// https://github.com/google/ko/blob/main/pkg/build/gobuild.go
// https://github.com/bazelbuild/rules_docker/tree/master/container
type container struct {
	*actions
}

func (c *container) pull(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		rname     string
		reference string
	)
	if err := starlark.UnpackArgs(
		"pull", args, kwargs,
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

	f, err := os.Create("image_base")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err := tarball.Write(ref, img, f); err != nil {
		return nil, err
	}

	// TODO: return starlark provider.
	return starlark.None, nil
}

func (c *container) image(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		rname       string
		rbase       string
		rentrypoint []string
		rfiles      []string
		rlabels     map[string]string
	)
	if err := starlark.UnpackArgs(
		"pull", args, kwargs,
		"name", &rname,
		"base", &rbase,
		"entrypoint", &rentrypoint,
		"files", &rfiles,
		"labels", &rlabels,
	); err != nil {
		return nil, err
	}

	basepath := rbase
	// TODO: load tag from provider?

	base := empty.Image
	if rbase != "" {
		// Load base from tar.

		// Load base from filesystem.
		img, err := tarball.ImageFromPath(basepath, &tag)
		if err != nil {
			panic(err)
		}
	}

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
			Author:    "laze",
			CreatedBy: "laze " + rname,
			//Comment:   "go build output, at " + appPath,
		},
	})

	// Augment the base image with our application layer.
	appImage, err := mutate.Append(base, layers...)
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
	cfg.Config.Entrypoint = rentrypoint
	//updatePath(cfg)
	cfg.Config.Env = append(cfg.Config.Env, "LAZE_DATA_PATH="+kodataRoot)
	cfg.Author = "github.com/emcfarlane/laze"

	if cfg.Config.Labels == nil {
		cfg.Config.Labels = map[string]string{}
	}
	for k, v := range rlabels {
		cfg.Config.Labels[k] = v
	}

	image, err := mutate.ConfigFile(withApp, cfg)
	if err != nil {
		return nil, err
	}

	//empty := v1.Time{}
	//if g.creationTime != empty {
	//	return mutate.CreatedAt(image, g.creationTime)
	//}

	f, err := os.Create("image")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err := tarball.Write(ref, img, f); err != nil {
		return nil, err
	}

	return nil, nil
}

func (c *container) push(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {

	imgpath := "image"

	tag, err := name.NewTag("gcr.io/foo/bar:latest", name.StrictValidation)
	if err != nil {
		return nil, err
	}

	// Load base from filesystem.
	img, err := tarball.ImageFromPath(imgpath, tag)
	if err != nil {
		return nil, err
	}

	reference := "gcr.io/foo/bar"
	ref, err := name.ParseReference(reference)
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
