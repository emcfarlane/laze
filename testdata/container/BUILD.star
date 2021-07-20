load("rules/container.star", "image", "pull")

# BASE_IMAGE
pull(
    name = "distroless",
    ref = "gcr.io/distroless/base@sha256:730c5f8abe534d84b11b6315ef0ee173661d3784b261ed8e6ba1f5885efc31d7",
)

# helloc is an image based on cross compiling packaing.
image(
    name = "helloc",
    base = "./distroless",
    entrypoint = ["/usr/bin/helloc"],
    labels = ["latest"],
    prioritized_files = ["/usr/bin/hello"],  # Supports estargz.
    tars = ["../packaging/helloc.tar"],
)

push(
    name = "myrepo",
    image = "./helloc",
    ref = "gcr.io/laze/helloc:latest",
)
