// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Code generated by devtools/genhelpdoc.go; DO NOT EDIT.

package main

const helpDoc = `
Audiorenamer traverses a directory and renames music tracks based on their
metadata. It extracts the track number and title from the files' metadata.
If the title contains slashes, it strips them out to create a valid filename.
The new filename format is "<track number>. <title>.<extension>".

The program takes a directory path as an required argument.

Running it on my music collection:

    $ audiorenamer ~/media/music
`
