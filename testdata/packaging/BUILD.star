load("languages/packaging.star", "tar")

tar(
    name "helloc.tar",
    strip_prefix = "testdata/cgo",
    package_dir = "/usr/bin",
    srcs = ["testdata/cgo/helloc?goarch=amd64&goos=linux"],
    mode = "0555",
)
