"bazeldnf repo integration test dependencies"

load("@bazeldnf//bazeldnf:defs.bzl", "rpm")

def bazeldnf_test_dependencies():
    rpm(
        name = "libvirt-libs-11.0.0-1.fc42.x86_64.rpm",
        sha256 = "aac272a2ace134b5ef60a41e6624deb24331e79c76699ef6cef0dca22d94ac7e",
        urls = [
            "https://kojipkgs.fedoraproject.org//packages/libvirt/11.0.0/1.fc42/x86_64/libvirt-libs-11.0.0-1.fc42.x86_64.rpm",
        ],
    )

    rpm(
        name = "libvirt-devel-11.0.0-1.fc42.x86_64.rpm",
        sha256 = "dba37bbe57903afe49b5666f1781eb50001baa81af4584b355db0b6a2afad9fa",
        urls = [
            "https://kojipkgs.fedoraproject.org//packages/libvirt/11.0.0/1.fc42/x86_64/libvirt-devel-11.0.0-1.fc42.x86_64.rpm",
        ],
    )
