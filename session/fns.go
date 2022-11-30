// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"fmt"
	"strings"
)

func verbose(f string, a ...interface{}) {
	v("session:"+f, a...)
}

// ParseBinds parses a CPU_NAMESPACE-style string to a
// slice of Bind structures.
func ParseBinds(s string) ([]Bind, error) {
	var b = []Bind{}
	if len(s) == 0 {
		return b, nil
	}
	binds := strings.Split(s, ":")
	for i, bind := range binds {
		if len(bind) == 0 {
			return nil, fmt.Errorf("bind: element %d is zero length", i)
		}
		// If the value is local=remote, len(c) will be 2.
		// The value might be some weird degenerate form such as
		// =name or name=. Both are considered to be an error.
		// The convention is to split on the first =. It is not up
		// to this code to determine that more than one = is an error
		// There is no rule that filenames can not contain an '='!
		c := strings.SplitN(bind, "=", 2)
		if len(c) == 2 {
			l, r := c[0], c[1]
			if len(r) == 0 {
				return nil, fmt.Errorf("bind: element %d:name in %q: zero-length remote name", i, bind)
			}
			if len(l) == 0 {
				return nil, fmt.Errorf("bind: element %d:name in %q: zero-length local name", i, bind)

			}
			b = append(b, Bind{Local: c[0], Remote: c[1]})
			continue
		}
		b = append(b, Bind{Local: c[0], Remote: c[0]})
	}
	return b, nil
}
