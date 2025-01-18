// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package restrict enables programs to utilize the [Landlock] Linux Security
// Module (LSM) for sandboxing on supported systems.
//
// On systems where Landlock is not available, this package will have no effect.
//
// [Landlock]: https://landlock.io
package restrict
