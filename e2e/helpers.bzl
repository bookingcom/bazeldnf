"A series of helpers for our e2e infrastructure"

load("@bazel_skylib//lib:paths.bzl", "paths")

def glob_workspace_files(workspace_path, extra_excludes = []):
    """Recursively globs the Bazel workspace files at the specified path.

    Args:
        workspace_path: A `string` representing the path to glob.
        extra_excludes: Other glob patterns to ignore

    Returns:
        A `list` of the files under the specified path ignoring certain Bazel
        artifacts (e.g. `bazel-*`).
    """
    return native.glob(
        [paths.join(workspace_path, "**", "*")],
        exclude = [
            paths.join(workspace_path, "bazel-*", "**"),
            paths.join(workspace_path, "MODULE.bazel.lock"),
        ] + extra_excludes,
    )
