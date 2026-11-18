// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build unix

package consent

import "golang.org/x/sys/unix"

func (r promptReader) Read(p []byte) (int, error) {
	fds := []unix.PollFd{
		{
			Fd:     int32(r.file.Fd()),
			Events: unix.POLLIN,
		},
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := unix.Poll(fds, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n != 0 {
			if err := r.ctx.Err(); err != nil {
				return 0, err
			}
			return r.file.Read(p)
		}
	}
}
