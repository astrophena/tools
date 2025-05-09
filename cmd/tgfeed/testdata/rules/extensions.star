# © 2024 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.


def filter_description(item):
    if not "media" in item.extensions:
        return False
    return (
        "Some"
        in item.extensions["media"]["group"][0]["children"]["description"][0]["value"]
    )


feeds = [
    feed(
        url="https://example.com/feed.xml",
        keep_rule=filter_description,
    )
]
