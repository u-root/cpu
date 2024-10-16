# cpu

[![CircleCI](https://circleci.com/gh/u-root/cpu.svg?style=svg)](https://circleci.com/gh/u-root/cpu)
[![Go Report Card](https://goreportcard.com/badge/github.com/u-root/cpu)](https://goreportcard.com/report/github.com/u-root/cpu)
[![CodeQL](https://github.com/u-root/cpu/workflows/CodeQL/badge.svg)](https://github.com/u-root/cpu/actions?query=workflow%3ACodeQL)
[![GoDoc](https://godoc.org/github.com/u-root/cpu?status.svg)](https://godoc.org/github.com/u-root/cpu)
[![License](https://img.shields.io/badge/License-BSD%203--Clause-blue.svg)](https://github.com/u-root/cpu/blob/master/LICENSE)

This repo is an implementation the Plan 9 cpu command, both client and server, for Linux.
More detail is available in the [CPU chapter of the LinuxBoot book](https://book.linuxboot.org/utilities/cpu.html).
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
Users are left juggling USB sticks and NVME cards, and this fails the first time there are
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

### Building your own docker container

You can easily build your own docker container to try things out.

```
docker build -t "${USER}/cpu:latest" .
```

or if you have installed a version of docker buildx, you can build a multi-arch manifest container and push it to docker hub:
```
% docker login
% docker buildx build --platform "linux/amd64,linux/arm64,linux/arm/v7" --progress plain --pull -t "${USER}/cpu:latest" .
```

### Pre-built Docker container for trying out cpu (on arm64 & amd64 for now)

We have created a docker container so you can try cpu client and server:
```
ghcr.io/u-root/cpu:main
```

It includes both the cpud (server) and cpu (client) commands. In the
container, you only have access to date and cat commands, but that is enough to get
the idea.

You will need keys. You can either use your own SSH keys that you use for
other things, for example:
```
export KEY=~/.ssh/id_rsa
export KEY=~/.ssh/a_special_key_for_this_docker
```

or generate one and use it.
```
ssh-keygen -f key -t rsa  -P ""
export KEY=`pwd`/key
```
NOTE! The name KEY is not required. Instead of KEY, you
can use any name you want, as long as you use it in the docker
command below.

To start the cpud, you need docker installed. Once that is done, you need to create
a docker network and start the daemon, with public and private keys.
The --mount option allows docker to provide the keys, using a bind mount
for both the private and public key.
That is how we avoid
storing keys in the container itself.
```
docker network create cpud
# If you ran docker before and it failed in some way, you may need to remove the
# old identity (e.g. docker rm cpud_test)
docker run --rm -v $KEY.pub:/key.pub -v $KEY:/key -v /tmp --name cpud_test --privileged=true -t -i -p 17010:17010 ghcr.io/u-root/cpu:main
```

Then you can try running a command or two by using the embedded cpu client in the docker container.  
_NOTE_: when you run cpu in this way, it does not immediately have access to your host's file system, 
which means you won't be able to really leverage the back-mount and you'll have to artificially set 
environment variables (like PWD) so that the remote task can execute.

```
docker exec -it -e PWD=/root cpud_test  /bin/cpu -key /key localhost /bin/date
```

Remember, this cpu command is running in the container. You need to use the name /key in the
container, not $KEY and you'll only be able to run binaries that have been pre-loaded into the 
container (which in the public container is /bin/cat and /bin/date)

To see the mounts:
```
docker exec -it -e PWD=/root cpud_test  /bin/cpu -key /key localhost /bin/cat /proc/mounts
```

You might want to just get a cpu command to let you talk to the docker cpud
directly:
```
go install github.com/u-root/cpu/cmds/cpu@latest
```

And now you can run
```
cpu -key $KEY localhost date
```

_NOTE_: if you are running on OSX, remember that your cpud docker is linux, so if you try the above
command you'll see:
```
$ cpu -key $KEY localhost date
2022/10/13 18:52:17 CPUD(as remote):fork/exec /bin/date: exec format error
```

To deal with that we'll have to play games with the namespace, which we'll cover next.

### cpu on heterogeneous systems.

The cpu command sets up the various 9p mounts with a default namespace. Users can override this
default with the -namespace switch. The argument to the switch looks like
PATH variables, with comma-separated values, but with one extra option: users can, optionally,
specify the local path and the remote path. This is useful when running ARM binaries
hosted from an x86 system.

In the example below, we show starting up a bash on an ARM system (solidrun honeycomb) using
a cpu command running on an x86 system.

```
cpu -namespace /home:/bin=`pwd`/bin:/lib=`pwd`/lib:/usr=`pwd`/usr honeycomb /bin/bash
```

Breaking this down, we set up the namespace so that:
* the remote /home is from our /home
* the remote /bin is from `pwd`/bin -- which, in this case, was an unpacked arm64 file system image
* the remote /lib is from `pwd`/lib
* the remote /usr is from `pwd`/usr

We can use the path /bin/bash, because /bin/bash on the remote points to `pwd`/bin/bash on the local
machine.

We can use the same trick to cpu to Linux from OSX, but instead of having an Arm tree under `pwd`
we'll need a Linux binary tree to pick binaries from.

### cpu over USB

There are many IoT like devices that do not have an ethernet port.
Fear not though: The Linux USB gadget drivers offer ethernet via USB!

There are [tutorials out
there](https://linuxlink.timesys.com/docs/wiki/engineering/HOWTO_Use_USB_Gadget_Ethernet), and here is the gist:

- enable the Linux kernel options
  * `CONFIG_USB_GADGET`
  * `CONFIG_USB_ETH`
  * `CONFIG_USB_ETH_RNDIS` (for Windows support)
  * `CONFIG_INET`
- add the MAC addresses for your gadget device and the machine you connect to in
  the kernel `CMDLINE`, e.g., `g_ether.dev_addr=12:34:56:78:9a:bc g_ether.host_addr=12:34:56:78:9a:bd`

## cpu will be familiar to ssh users

As mentioned, cpu looks and feels a lot like ssh, to the point of honoring ssh config files.
For the honeycomb, for example, the ssh config entry looks like this (we shorten the name to 'h'
for convenience):

```
Host h
	HostName honeycomb
	Port 17010
	User root
	IdentityFile ~/.ssh/apu2_rsa
```

Note that the cpu command is itself a 9p server; i.e., your instance of cpu runs your server. The remote
cpu server may run as root, but all file accesses happen locally as you. Hence,
the cpu command does not grant greater access to the local machine than you already possess.
I.e., there is no privilege escalation.

## cpu and Docker

Maintaining file system images is inconvenient.
We can use Docker containers on remote hosts instead.
We can take a standard Docker container and, with suitable options, use docker
to start the container with cpu as the first program it runs.

That means we can use any Docker image, on any architecture, at any time; and
we can even run more than one at a time, since the namespaces are private.

In this example, we are starting a standard Ubuntu image:
```
docker run -v /home/rminnich:/home/rminnich -v /home/rminnich/.ssh:/root/.ssh -v /etc/hosts:/etc/hosts --entrypoint /home/rminnich/go/bin/cpu -it ubuntu@sha256:073e060cec31fed4a86fcd45ad6f80b1f135109ac2c0b57272f01909c9626486 h
Unable to find image 'ubuntu@sha256:073e060cec31fed4a86fcd45ad6f80b1f135109ac2c0b57272f01909c9626486' locally
docker.io/library/ubuntu@sha256:073e060cec31fed4a86fcd45ad6f80b1f135109ac2c0b57272f01909c9626486: Pulling from library/ubuntu
a9ca93140713: Pull complete
Digest: sha256:073e060cec31fed4a86fcd45ad6f80b1f135109ac2c0b57272f01909c9626486
Status: Downloaded newer image for ubuntu@sha256:073e060cec31fed4a86fcd45ad6f80b1f135109ac2c0b57272f01909c9626486
WARNING: The requested image's platform (linux/arm64/v8) does not match the detected host platform (linux/amd64) and no specific platform was requested
1970/01/01 21:37:32 CPUD:Warning: mounting /tmp/cpu/lib64 on /lib64 failed: no such file or directory
# ls
bbin  buildbin	env  go    init     lib    proc  tcz  ubin  var
bin   dev	etc  home  key.pub  lib64  sys	 tmp  usr
#
```

Note that the image was updated and then started. The /lib64 mount fails, because there is no /lib64 directory in the image, but
that is harmless.

On the local host, on which we ran docker, this image will show up in docker ps:
```rminnich@a300:~$ docker ps
CONTAINER ID   IMAGE     COMMAND                  CREATED         STATUS         PORTS     NAMES
b92a3576229b   ubuntu    "/home/rminnich/go/b…"   9 seconds ago   Up 9 seconds             inspiring_mcnulty
````

Even though the binaries themselves are running on the remote ARM system.

## Testing with vsock

Vsock is a useful transport layer available in Linux, and support by at least QEMU.

We use the mdlayher/vsock package.

In the cpu and cpud, the switch
```
-net vsock
```
will enable vsock.

In the host kernel, you need ```vhost_vsock``` module:
```
sudo modprobe vhost_vsock
```
.

When starting qemu, add
```
-device vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=3
```
to the command line. The '3' is arbitrary; it just needs to be agreed upon on both sides.

When running a cpu command, the host name is the vsock guest-cid you specified in qemu:
```
cpu -net vsock 3 date
```

If you want a different port, you can use the same -sp switch you use for other network types.

## De-centralized cpu with DNS-SD

A variation of the cpu and cpud commands now exist which use dns-sd to autodiscover and register cpu resources.  DNS-SD is a multi-cast DNS protocol that will multi-cast out requests for resources and get responses from participating cpud nodes.  Meta-data provides information such as architecture, OS, number of cores, free memory, load average, and number of existing cpu clients.  When you start a decpud you can specify additional meta-data which might be harder to auto-disocver such as near=storage, near=gateway, near=gpu, or secure=true (if running in a confidential computing domain).

In order to use this functionality, you can use decpu and decpud just as you would cpu and cpud.  decpud will enable dns-sd registration by default, and multicast its information in response to requests.  In order to use this from decpu, you can just specify decpu without a hostname and, by default, it will find the lowest loaded decpud with the same architecture and OS as the host you run the decpu command from.

You can specify additional constraints by using a dns-sd URI formulation:
```
  decpu dnssd://?requirement=value\&otherrequirement=\<othervalue\&sort=tenet
```

Essentially you can provide a set of key/value requirements that must be met in order for a decpud node to be considered.  You can override the defaults (arch/os), or you can specify a minimum core count or minimum amount of free memory.  Numeric values can use comparison operators (< or >) Finally, you can specify any number of numeric keys to sort based on.  If you want to ignore one of the default requirements (e.g. arch, os) then you can set to a \*:
```
  decpu dnssd://?arch=\*\&os=\*
```
 This can be useful if what you are running is a script or an interpretive language like python.  _If you are running on the shell, make sure you escape any characters the shell may have an interest in (<, >, !, &, *)_

### Runnign the DNS-SD tools inside Docker

In order to use dns-sd you have to be able to send/receive multicast.  The easiest way to do this on Linux is to use host networking when you start your docker containers (--network host).  This unfortunately does not work on Mac OSX, so you will need to run a relay on your system that tunnels multicast to/from the docker network.  The relay client can be run in every container or you can start all dns-sd containers in the same docker network and start a relay container client on that network to communicate for the group.

There is a container in the decent-e fork of the dnssd package: https://github.com/decent-e/dnssd under cmd/relay.  Running relay will start the server on the host, and running relay mode=client will start the client inside the docker container.  You can also use a pre-build docker image ( docker pull ghcr.io/decent-e/dnssd:main ) to start the client in a docker.

Example: (on mac)
```
# presumes you have already setup your $KEY appropriately
% docker create network cpud
% go install github.com/decent-e/dnssd/cmd/relay@latest
% export PATH=$GOBIN:$PATH
% relay &
% docker run -d --network cpud ghcr.io/decent-e/dnssd:main
% docker run -d --network cpud -v $KEY:/key -v $KEY.pub:/key.pub -v /tmp:/tmp --privileged --rm --name decpud ghcr.io/decent-e/cpu:decent-e /bin/decpud
% docker exec -i -t -e PWD=/ decpud /bindecpu -key /key . /bin/date
# you should also be able to see the service from your mac
% dns-sd -B _ncpu._tcp
Browsing for _ncpu._tcp
DATE: ---Sun 09 Oct 2022---
18:35:56.925  ...STARTING...
Timestamp     A/R    Flags  if Domain               Service Type         Instance Name
18:35:56.925  Add        3  17 local.               _ncpu._tcp.          d3c196958c24-cpud
18:35:56.925  Add        2  14 local.               _ncpu._tcp.          d3c196958c24-cpud
```

## Summary
The cpu command makes using small embedded systems dramatically easier. There is no need to install
a distro, or juggle distros; there is no need to scp files back and forth; just run commands
as needed.

## Development

For debugging, `tcpdump` is very handy. Read [a short tutorial](
https://danielmiessler.com/study/tcpdump/) to get familiar with it.

## Further reading

### Talks

* Short Talk "building small stateless network-controlled appliances with
  coreboot/linuxboot and u-root’s cpu command"
  * at Open Source Firmware Conference 2019
   [slides](https://docs.google.com/presentation/d/1ee8kxuLBJAyAi-xQqE75EMk-lNM5d8Z6CarWoYlO6Ws/edit?usp=sharing) /
   [recording](https://www.youtube.com/watch?v=mxribsZFDQQ)
  * at BARC2021
   [slides (PDF)](https://bostonarch.github.io/2021/presentations/U-root%20CPU%20command.pdf)
* [Network Managed Processors at IoT World 2021](https://docs.google.com/presentation/d/1jREHiHci1EAMWdj--6uX9o0aVpeCPy21UQpnl1oTSwE/edit?usp=sharing)
* ["Plan 9 CPU command, in Go, for Linux - the network is the computer -- for real this time" at FOSDEM 2022
  ](https://fosdem.org/2022/schedule/event/plan_9_cpu_cmd/)
* ["Drivers From Outer Space at CLT 2022 - Fast, Simple Driver Development"
  ](https://chemnitzer.linux-tage.de/2022/de/programm/beitrag/226)
* Short demo of [Attaching CPUs via USB](https://media.ccc.de/v/all-systems-go-2023-246-attaching-cpus-via-usb)

### History

The first version of cpu was developed for Plan 9, and is described [here](http://man.cat-v.org/plan_9/1/cpu).
