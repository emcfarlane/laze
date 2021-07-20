load("rule.star", "attr", "rule")

def _tar_impl(ctx):
    # TODO: providers list?
    return ctx.actions.packaging.tar(
        name = ctx.attrs.name,
        strip_prefix = ctx.attrs.strip_prefix,
        package_dir = ctx.attrs.package_dir,
        srcs = ctx.attrs.srcs,
    )

tar = rule(
    impl = _tar_impl,
    attrs = {
        "strip_prefix": attr.string(),
        "package_dir": attr.string(default = "/"),
        "srcs": attr.label_list(mandatory = True),
    },
)

#def _tar_header_impl(ctx):
#    # TODO: providers list...
#    return struct(
#        name = ctx.attr.name,
#        src = ctx.attr.src,
#        mode = ctx.attr.mode,
#    )
#
## tar_header name is the path?
#tar_header = rule(
#    impl = _tar_header_impl,
#    attrs = {
#        "src": ttr.file(allow_files = True, mandatory = True),
#        "mode": attr.int(default = 0o600),
#    },
#)
