load("rules/proto.star", "protoc", "protoc_plugin")

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
