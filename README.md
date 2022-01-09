# cpu

[![CircleCI](https://circleci.com/gh/u-root/cpu.svg?style=svg)](https://circleci.com/gh/u-root/cpu)
[![Go Report Card](https://goreportcard.com/badge/github.com/u-root/cpu)](https://goreportcard.com/report/github.com/u-root/cpu)
[![GoDoc](https://godoc.org/github.com/u-root/cpu?status.svg)](https://godoc.org/github.com/u-root/cpu)
[![License](https://img.shields.io/badge/License-BSD%203--Clause-blue.svg)](https://github.com/u-root/cpu/blob/master/LICENSE)

This repo is an implementation the Plan 9 cpu command, both client and server, for Linux.
More detail is available in the [CPU chapter of the LinuxBoot book](https://book.linuxboot.org/cpu/).
Unlike the Plan 9 command, this version uses the ssh protocol for the underlying transport. It includes
features familiar to ssh users, such as support for the ssh config file.

## Overview
The cpu command 
lets you log in from a local system to a remote system and see some or all of the files (how much is
up to you) from the local system. 

This is wonderfully convenient for embedded systems programmers. Because some or all the files
can come from your local machine, including binaries, the only thing you need installed 
on the remote machine is the cpu daemon itself. 

### Motivation
Consider the case of running a
complex Python program on an embedded system.
We will need to either do a full install of some distro on that system, meaning we
need USB ports and local storage; or we will need to run the program over the
network.

Installing distros can turn into a mess. Some programs only work under specific distros. 
In some cases, when two programs are needed in a pipeline, it can happen that they only work
under different distros!
Users are left juggling USB stucks and NVME cards, and this fails the first time there are
two programs which need two different distros.

Running over a network is usually done with ssh, but ssh can not supply the programs and files.
We would need to either set up a network file system, meaning 
finding a sysadmin willing to set it up, and keep it working; or, trying to figure out which 
files the program needs, and using rsync or scp to get them there. In some cases, 
the target system might not have enough memory to hold those files!

Cpu looks like ssh, but with an important difference: it also provides a file transport
so that the files your program needs are available via a 9p mount. For example, if I have an
embedded system named camera, and I need to read the flash with the flashrom command, I simply type:

```
cpu camera flashrom -r rom.img
```

<img alt="IP camera robots" src="doc/img/ip-camera-robot.jpg" height="240px" />

Breaking this down: cpu is the cpu command; camera is the host name; flashrom is the command
to run; the options are to do a read (-r) into a file called rom.img.

Where does that file end up? In whatever of my home directories I ran the cpu command from. I need
not worry about scp'ing it back, or any such thing; it's just there. 

### cpu on heterogeneous systems.

The cpu command sets up the various 9p mounts with a default namespace. Users can override this
default by setting the CPU_NAMESPACE environment variable. This variable looks like most
PATH variables, with comma-separated values, but with one extra option: users can, optionally,
specify the local path and the remote path. This is useful when running ARM binaries 
hosted from an x86 system. 

In the example below, we show starting up a bash on an ARM system (solidrun honeycomb) using
a cpu command running on an x86 system. 

```
CPU_NAMESPACE=/home:/bin=`pwd`/bin:/lib=`pwd`/lib:/usr=`pwd`/usr cpu honeycomb /bin/bash
```

Breaking this down, we set up CPU_NAMESPACE so that:
* the remote /home is from our /home
* the remote /bin is from `pwd`/bin -- which, in this case, was an unpacked arm64 file system image
* the remote /lib is from `pwd`/lib
* the remote /usr is from `pwd`/usr

We can use the path /bin/bash, because /bin/bash on the remote points to `pwd`/bin/bash on the local
machine.

## cpu will be familiar to ssh users

As mentioned, cpu looks and feels a lot like ssh, to the point of honoring ssh config files.
For the honeycomb, for example, the ssh config entry looks like this (we shorten the name to 'h'
for convenience):

```
Host h
	HostName honeycomb
	Port 23
	User root
	IdentityFile ~/.ssh/apu2_rsa
```

Note that the cpu command is itself a 9p server; i.e., your instance of cpu runs your server. The remote 
cpu server may run as root, but all file accesses happen locally as you. Hence, 
the cpu command does not grant greater access to the local machine than you already possess. 
I.e., there is no privilege escalation.

## Summary
The cpu command makes using small embedded systems dramatically easier. There is no need to install
a distro, or juggle distros; there is no need to scp files back and forth; just run commands
as needed.

## Further reading

### Talks

* [Open Source Firmware Conference Short Talk](https://docs.google.com/presentation/d/1ee8kxuLBJAyAi-xQqE75EMk-lNM5d8Z6CarWoYlO6Ws/edit?usp=sharing)
  * [recording](https://www.youtube.com/watch?v=mxribsZFDQQ)
  * [PDF from BARC2021](https://bostonarch.github.io/2021/presentations/U-root%20CPU%20command.pdf)

* [Network Managed Processors at IoT World 2021](https://docs.google.com/presentation/d/1jREHiHci1EAMWdj--6uX9o0aVpeCPy21UQpnl1oTSwE/edit?usp=sharing)

### History

The first version of cpu was developed for Plan 9, and is described [here](http://man.cat-v.org/plan_9/1/cpu).
