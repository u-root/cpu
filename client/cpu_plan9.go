// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"os"

	"github.com/hugelgupf/p9/p9"
)

func osflags(fi os.FileInfo, mode p9.OpenFlags) int {
	flags := int(mode)

	// special processing goes here.

	return flags
}
