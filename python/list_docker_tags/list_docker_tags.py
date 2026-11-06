#!/usr/bin/env python3

from argparse import ArgumentParser, Namespace
from json import dumps, load
from logging import DEBUG, Formatter, StreamHandler, getLogger
from urllib import parse, request

# pylint: disable=missing-class-docstring
# pylint: disable=missing-function-docstring

log = getLogger(name=__name__)
log.setLevel(DEBUG)

console = StreamHandler()
console.setLevel(DEBUG)
formatter = Formatter(
    fmt="%(asctime)s %(filename)s[line:%(lineno)d] %(levelname)s %(message)s",
    datefmt="%a, %d %b %Y %H:%M:%S",
)
console.setFormatter(formatter)
log.addHandler(console)


class ListTags:
    def __init__(self, images) -> None:
        self.images = images

    def auth(self, image: str) -> str:
        params = parse.urlencode(
            {"service": "registry.docker.io", "scope": f"repository:{image}:pull"}
        )
        req = request.Request(url=f"https://auth.docker.io/token?{params}")
        with request.urlopen(req) as _rep:
            return load(_rep).get("token")

    def _list_tags(self, image: str):
        repo = image if "/" in image else f"library/{image}"
        req = request.Request(
            url=f"https://registry-1.docker.io/v2/{repo}/tags/list",
            headers={
                "Content-Type": "application/json; charset=utf-8",
                "Authorization": f"Bearer {self.auth(repo)}",
            },
        )
        with request.urlopen(req) as _rep:
            return load(_rep)

    def list_tags(self) -> tuple:
        if isinstance(self.images, str):
            return (self._list_tags(self.images),)
        return tuple(self._list_tags(image) for image in self.images)


if __name__ == "__main__":
    arg = ArgumentParser(description="list docker tags.")
    arg.add_argument(
        "-i",
        "--indent",
        action="store_true",
        help="indent or not.",
    )
    arg.add_argument("images", nargs="+", type=str)
    parses: Namespace = arg.parse_args()

    tags = ListTags(parses.images).list_tags()
    indent = 2 if parses.indent else None
    for img, version in zip(parses.images, tags):
        log.info(
            "%s",
            dumps(
                [f"{img}:{tag}" for tag in (version.get("tags") or [])],
                indent=indent,
            ),
        )
