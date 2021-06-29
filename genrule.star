

def _genrule_impl(ctx):
    return []

genrule = rule(
    implementation = _genrule_impl,
    attrs = {
        "deps": attr.label_list(),
        ...
    },
)
