"""bazeldnf public dependency for WORKSPACE"""

load(
    "@bazeldnf//bazeldnf:repositories.bzl",
    _bazeldnf_dependencies = "bazeldnf_dependencies",
    _bazeldnf_register_toolchains = "bazeldnf_register_toolchains",
)
load(
    "@bazeldnf//internal:rpm.bzl",
    _rpm = "rpm",
)

rpm = _rpm

def bazeldnf_dependencies():
    """bazeldnf dependencies when consuming the repo externally"""
    _bazeldnf_dependencies()
    _bazeldnf_register_toolchains(name = "bazeldnf", bazeldnf_version = "0.5.9")
