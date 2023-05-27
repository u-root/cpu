# p9cpu

`p9cpu` is an implementation of the Plan 9 `cpu` command for Linux, similar to
[u-root/cpu](https://github.com/u-root/cpu). Check the
[CPU chapter of the LinuxBoot book](https://book.linuxboot.org/cpu/) for more
details. Compared with the original Plan 9 `cpu` and the Go version
[u-root/cpu](https://github.com/u-root/cpu), `p9cpu` is written in Rust and
based on [tokio](https://tokio.rs/). It uses gRPC for the underlying transport.

## Build

```sh
cargo build --release
```

## Run

Start the server in a VM with vsock:

```sh
./p9cpud --net vsock
```

Use the `p9cpu` command to run the `bash` command (of the host) in the VM:

```sh
./p9cpu --net vsock --tty --tmp-mnt /tmp --namespace /lib:/lib64:/usr:/bin:/home:/etc $VM_VSOCK_CID -- bash
```

Check `p9cpud --help` and `p9cpu --help` for more details.
