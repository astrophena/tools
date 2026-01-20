# © 2026 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.


def format(item):
    return "Single: " + item.title


feed(
    url="https://example.com/feed.xml",
    format=format,
)
