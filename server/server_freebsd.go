// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"os"
	"os/exec"
)

// cpud can run in one of three modes
// o init
// o daemon started by init
// o manager of one cpu session.
func init() {
	// placeholder. It's not clear we ever want to do this. We used to create
	// a root file system here, but that should be up to the server. The files
	// might magically exist, b/c of initrd; or be automagically mounted via
	// some other mechanism.
	if os.Getpid() == 1 {
		verbose("PID 1")
	}
}

func command(n string, args ...string) *exec.Cmd {
	return exec.Command(n, args...)
}
