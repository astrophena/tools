# © 2024 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

feeds = [
    feed(
        url = "https://example.com/feed.xml",
        block_rule = lambda item: "block" in item.title.lower(),
    )
]
