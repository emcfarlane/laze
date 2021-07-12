package laze

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"time"

	"go.starlark.net/starlark"
)

type packaging struct {
	*actions
}

//pkg_tar(
//    name = "bazel-bin",
//    strip_prefix = "/src",
//    package_dir = "/usr/bin",
//    srcs = ["//src:bazel"],
//    mode = "0755",
//)

// Create a tarball...
func (p *packaging) tar(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name string
		mode int64
	)
	if err := starlark.UnpackArgs(
		"tar", args, kwargs,
		"name", &name, "mode", &mode,
	); err != nil {
		return nil, err
	}

	creationTime := time.Time{} // zero

	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	addFile := func(name string) error {
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			return err
		}

		header := &tar.Header{
			Name:     name,
			Size:     stat.Size(),
			Typeflag: tar.TypeReg,
			// Use a fixed Mode, so that this isn't sensitive to the directory and umask
			// under which it was created. Additionally, windows can only set 0222,
			// 0444, or 0666, none of which are executable.
			Mode:    mode,
			ModTime: creationTime,
		}
		// write the header to the tarball archive
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// copy the file data to the tarball
		if _, err := io.Copy(tw, file); err != nil {
			return err
		}
		return nil
	}

	for _, file := range files {
		if err := addFile(file); err != nil {
			return nil, err
		}
	}

	// Loop over files and create tarball
	return starlark.None, nil
}
