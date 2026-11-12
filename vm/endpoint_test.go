// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// An internal test, because where the dial would be made is decided by
// cpuCmd, and CPUCommand itself dials, which needs a running guest.

package vm

import (
	"strings"
	"testing"

	"github.com/u-root/cpu/client"
)

// The socket path must reach the dial exactly as the caller gave it.
//
// client.Command runs the name it is given through GetHostUser before any
// option applies, and that does two things to a path. It cuts on "@", so a
// socket under a directory such as "workspace@2" keeps only what follows.
// Then it asks ssh_config for a HostName, where an ordinary wildcard rule
// such as "Host !127.0.0.1 !localhost *" matches a path -- "*" matches "/" --
// and rewrites it to a host name. Either way Dial would be made against
// something that is not the socket, so the path must never be the argument to
// Command.
func TestTheSocketPathReachesTheDialUnchanged(t *testing.T) {
	for _, sock := range []string{
		"/tmp/s/c0",
		// An "@" in the path: Jenkins makes directories like this.
		"/home/jenkins/workspace@2/c0",
		// A path a wildcard ssh_config rule would otherwise rewrite.
		"/tmp/build/s/c-",
	} {
		i := &Image{sock: sock}
		cpu, err := i.cpuCmd("/bin/true")
		if err != nil {
			t.Errorf("cpuCmd with socket %q: got %v, want nil", sock, err)
			continue
		}
		if cpu.HostName != sock {
			t.Errorf("socket %q: HostName is %q, and that is what Dial uses", sock, cpu.HostName)
		}
		if cpu.Host != sock {
			t.Errorf("socket %q: Host is %q", sock, cpu.Host)
		}
	}
}

// Without a socket the endpoint is the loopback address and the default port,
// exactly as it was before WithSocket existed.
func TestWithoutASocketTheEndpointIsUnchanged(t *testing.T) {
	i := &Image{}
	cpu, err := i.cpuCmd("/bin/true")
	if err != nil {
		t.Fatalf("cpuCmd: got %v, want nil", err)
	}
	if cpu.HostName != "127.0.0.1" {
		t.Errorf("HostName = %q, want %q", cpu.HostName, "127.0.0.1")
	}
	if cpu.Port != client.DefaultPort {
		t.Errorf("Port = %q, want %q", cpu.Port, client.DefaultPort)
	}
}

// A socket path is never handed to client.Command, which is the rule the two
// tests above depend on. This states it directly: Command's own treatment of
// such a path is lossy, so the fix is to not call it with one.
func TestCommandItselfWouldMangleASocketPath(t *testing.T) {
	const sock = "/home/jenkins/workspace@2/c0"
	if got := client.Command(sock, "/bin/true").HostName; got == sock {
		t.Skip("client.Command no longer rewrites such a path; the guard in cpuCmd may be removable")
	} else if !strings.HasSuffix(sock, got) {
		t.Logf("client.Command(%q) gives HostName %q", sock, got)
	}
}
