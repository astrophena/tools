# © 2025 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

# vim: ft=starlark shiftwidth=4 tabstop=4

def hello(name = "world"):
    """
    Greets the user.
    """
    print("Hello, %s!" % name)
