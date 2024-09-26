// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mount

import (
	"bufio"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func init() {
	for k, v := range map[string]uintptr{
		"mnt_acls":        unix.MNT_ACLS,
		"mnt_async":       unix.MNT_ASYNC,
		"mnt_automounted": unix.MNT_AUTOMOUNTED,
		"mnt_byfsid":      unix.MNT_BYFSID,
		"mnt_cmdflags":    unix.MNT_CMDFLAGS,
		"mnt_defexported": unix.MNT_DEFEXPORTED,
		"mnt_delexport":   unix.MNT_DELEXPORT,
		"mnt_exkerb":      unix.MNT_EXKERB,
		"mnt_exportanon":  unix.MNT_EXPORTANON,
		"mnt_exported":    unix.MNT_EXPORTED,
		"mnt_expublic":    unix.MNT_EXPUBLIC,
		"mnt_exrdonly":    unix.MNT_EXRDONLY,
		"mnt_force":       unix.MNT_FORCE,
		"mnt_gjournal":    unix.MNT_GJOURNAL,
		"mnt_ignore":      unix.MNT_IGNORE,
		"mnt_lazy":        unix.MNT_LAZY,
		"mnt_local":       unix.MNT_LOCAL,
		"mnt_multilabel":  unix.MNT_MULTILABEL,
		"mnt_nfs4acls":    unix.MNT_NFS4ACLS,
		"mnt_noatime":     unix.MNT_NOATIME,
		"mnt_noclusterr":  unix.MNT_NOCLUSTERR,
		"mnt_noclusterw":  unix.MNT_NOCLUSTERW,
		"mnt_noexec":      unix.MNT_NOEXEC,
		"mnt_nonbusy":     unix.MNT_NONBUSY,
		"mnt_nosuid":      unix.MNT_NOSUID,
		"mnt_nosymfollow": unix.MNT_NOSYMFOLLOW,
		"mnt_nowait":      unix.MNT_NOWAIT,
		"mnt_quota":       unix.MNT_QUOTA,
		"mnt_rdonly":      unix.MNT_RDONLY,
		"mnt_reload":      unix.MNT_RELOAD,
		"mnt_rootfs":      unix.MNT_ROOTFS,
		"mnt_snapshot":    unix.MNT_SNAPSHOT,
		"mnt_softdep":     unix.MNT_SOFTDEP,
		"mnt_suiddir":     unix.MNT_SUIDDIR,
		"mnt_suj":         unix.MNT_SUJ,
		"mnt_suspend":     unix.MNT_SUSPEND,
		"mnt_synchronous": unix.MNT_SYNCHRONOUS,
		"mnt_union":       unix.MNT_UNION,
		"mnt_untrusted":   unix.MNT_UNTRUSTED,
		"mnt_update":      unix.MNT_UPDATE,
		"mnt_updatemask":  unix.MNT_UPDATEMASK,
		"mnt_user":        unix.MNT_USER,
		"mnt_verified":    unix.MNT_VERIFIED,
		"mnt_visflagmask": unix.MNT_VISFLAGMASK,
		"mnt_wait":        unix.MNT_WAIT,
	} {
		convert[k] = v
	}
}

// Mount takes a full fstab as a string and does whatever mounts are needed.
// It ignores comment lines, and lines with less than 6 fields. In principal,
// Mount should be able to do a full remount with the contents of /proc/mounts.
// Mount makes a best-case effort to mount the mounts passed in a
// string formatted to the fstab standard.  Callers should not die on
// a returned error, but be left in a situation in which further
// diagnostics are possible.  i.e., follow the "Boots not Bricks"
// principle.
func Mount(fstab string) error {
	return fmt.Errorf("??")
}

func mount(m mounter, fstab string) error {
	var lineno int
	s := bufio.NewScanner(strings.NewReader(fstab))
	var err error
	for s.Scan() {
		lineno++
		l := s.Text()
		if strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Fields(l)
		// fstab is historical, pretty free format.
		// Users may have dropped a random fstab in and we need
		// to be forgiving.
		// The last two fields no longer have any meaning or use.
		if len(f) < 6 {
			continue
		}

		// fstab fields:
		// /dev/disk/by-uuid/c0d2b09d-5330-4d08-a787-6e0e95592bf3 /boot ext4 defaults 0 0
		// what to mount, where to mount, fstype, options
		// We do need NOT to set MS_PRIVATE, since we've done a successful unshare.
		// This note is here in case someone gets confused in the future.
		// Setting MS_PRIVATE will get an EINVAL.
		dev, where, fstype, opts := f[0], f[1], f[2], f[3]

		// surprise! It turns out that correct behavior from mount is to follow symlinks
		// on where and device and use that. That's why /bin -> /usr/bin gets mounted
		// correctly.
		if w, err := filepath.EvalSymlinks(where); err == nil {
			where = w
		}
		if w, err := filepath.EvalSymlinks(dev); err == nil {
			dev = w
		}

		// The man page implies that the Linux kernel handles flags of "defaults"
		// we do no further manipulation of opts.
		flags, data := parse(opts)
		if e := m(dev, where, fstype, flags, data); e != nil {
			err = errors.Join(err, fmt.Errorf("Mount(%q, %q, %q, %q=>(%#x, %q)): %v", dev, where, fstype, opts, flags, data, e))
		}
	}
	return err
}
