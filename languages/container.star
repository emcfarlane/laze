load("rule.star", "attr", "rule")

def _container_pull_impl(ctx):
    ctx.actions.container_pull(
        name = "",
        // 
    )
    # TODO: providers list...
    return struct(
        outs = [ctx.build_dir + "/" + ctx.attrs.name],
    )

container_pull = rule(
    impl = _container_pull_impl,
    attrs = {
        "digest": attr.string(),
        "registry": attr.string(),
        "repository": attr.string(),
    },
)

def _container_impl(ctx):
    ctx.actions.container(
        name = "",
        // 
    )
    # TODO: providers list...
    return struct(
        outs = [ctx.build_dir + "/" + ctx.attrs.name],
    )

container = rule(
    impl = _container_impl,
    attrs = {
        "digest": attr.string(),
        "registry": attr.string(),
        "repository": attr.string(),
    },
)
