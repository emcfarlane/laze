load("languages/proto.star", "protoc", "protoc_plugin")
#load("languages/go.star", "protoc_go")

protoc_plugin(
    name = "protoc_go",
    args = [
        "--go_out=paths=source_relative:.",
    ],
)

protoc(
    name = "books",
    srcs = ["api.proto"],
    plugins = [
        "protoc_go",
    ],
)
