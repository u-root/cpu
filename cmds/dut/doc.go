// dut manages Devices Under Test (a.k.a. DUT) from a host.
// A primary goal is allowing multiple hosts with any architecture to connect.
//
// This program was designed to be used in u-root images, as the uinit,
// or in other initramfs systems. It can not function as a standalone
// init: it assumes network is set up, for example.
//
// In this document, dut refers to this program, and DUT refers to
// Devices Under Test. Hopefully this is not too confusing, but it is
// convenient. Also, please note: DUT is plural (Devices). We don't need
// to say DUTs -- at least one is assumed.
//
// The same dut binary runs on host and DUT, in either device mode (i.e.
// on the DUT), or in some host-specific mode. The mode is chosen by
// the first non-flag argument. If there are flags specific to that mode,
// they follow that argument.
// E.g., when uinit is run on the host and we want it to enable cpu daemons
// on the DUT, we run it as follows:
// dut cpu -key ...
// the -key switch is only valid following the cpu mode argument.
//
// modes
// dut currently supports 3 modes.
//
// The first, default, mode, is "device". In device mode, dut makes an http connection
// to a dut running on a host, then starts an  HTTP RPC server.
//
// The second mode is "tester". In this mode, dut calls the Welcome service, followed
// by the Reboot service. Tester can be useful, run by a shell script in a for loop, for
// ensure reboot is reliable.
//
// The third mode is "cpu". dut will direct the DUT to start a cpu service, and block until
// it exits. Flags for this service:
// pubkey: name of the public key file
// hostkey: name of the host key file
// cpuport: port on which to serve the cpu service
//
// Theory of Operation
// dut runs on the host, accepting connections from DUT, and controlling them via
// Go HTTP RPC commands. As each command is executed, its response is printed.
// Commands are:
//
// Welcome -- get a welcome message
// Argument: None
// Return: a welcome message in cowsay format:
// < welcome to DUT >
//   --------------
//          \   ^__^
//           \  (oo)\_______
//              (__)\       )\/\
//                  ||----w |
//                  ||     ||
//
// Die -- force dut on DUT to exit
// Argument: time to sleep before exiting as a time.Duration
// Return: no return; kills the program running on DUT
//
// Reboot
// Argument: time to sleep before rebooting as a time.Duration
//
// CPU -- Start a CPU server on DUT
// Arguments: public key and host key as a []byte, service port as a string
// Returns: returns (possibly nil) error exit value of cpu server; blocks until it is done
//
//
package main
