load("concat.star", "concat")

concat(
    name = "sh",
    out = "text.txt",
    chunks = [
        "into.txt",
        "body.txt",
    ],
    #select("os", {
    #    "//conditions:default": "//actions_run:merge_on_linux",
    #    "on_linux": "//actions_run:merge_on_linux",
    #    "on_windows": "//actions_run:merge_on_windows.bat",
    #}),
    merge_tool = {
        "linux": "merge.sh",
        "windows": "merge.bat",
    }.get(
        env.os,
        "merge.sh",
    ),
)
