// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.

	config "github.com/kevinburke/ssh_config"
	"github.com/u-root/cpu/client"
	"github.com/u-root/cpu/ds"
	"github.com/u-root/u-root/pkg/ulog"

	// We use this ssh because it can unpack password-protected private keys.
	ossh "golang.org/x/crypto/ssh"
)

const defaultPort = "17010"

var (
	defaultKeyFile = filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa")
	debug          = flag.Bool("d", false, "enable debug prints")
	dbg9p          = flag.Bool("dbg9p", false, "show 9p io")
	dump           = flag.Bool("dump", false, "Dump copious output, including a 9p trace, to a temp file at exit")
	fstab          = flag.String("fstab", "", "pass an fstab to the cpud")
	hostKeyFile    = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	keyFile        = flag.String("key", "", "key file")
	namespace      = flag.String("namespace", "/lib:/lib64:/usr:/bin:/etc:/home", "Default namespace for the remote process -- set to none for none")
	network        = flag.String("net", "", "network type to use. Defaults to whatever the cpu client defaults to")
	numCPUs        = flag.Int("n", 1, "number CPUs to run on")
	sp             = flag.String("sp", "", "cpu default port")
	root           = flag.String("root", "/", "9p root")
	timeout9P      = flag.String("timeout9p", "100ms", "time to wait for the 9p mount to happen.")
	ninep          = flag.Bool("9p", true, "Enable the 9p mount in the client")
	// Special variant flags
	dockerSocket    = flag.String("docker-socket", "/var/run/docker.sock", "Path to Docker socket for decker variant")
	composeFile     = flag.String("compose-file", "docker-compose.yml", "Path to docker-compose.yml for decompose variant")
	helmCharts      = flag.String("helm-charts", "", "Path to Helm charts directory for delm variant")
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v          = func(string, ...interface{}) {}
	dumpWriter *os.File
)

// Installation instructions for different platforms
var installInstructions = map[string]map[string]string{
	"docker": {
		"darwin": `Install Docker Desktop for Mac:
1. Visit https://www.docker.com/products/docker-desktop
2. Download and install Docker Desktop
3. Start Docker Desktop from your Applications folder`,
		"linux": `Install Docker Engine:
1. Update package index:
   sudo apt-get update
2. Install prerequisites:
   sudo apt-get install ca-certificates curl gnupg
3. Add Docker's official GPG key:
   sudo install -m 0755 -d /etc/apt/keyrings
   curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
   sudo chmod a+r /etc/apt/keyrings/docker.gpg
4. Add Docker repository:
   echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
5. Install Docker Engine:
   sudo apt-get update
   sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin`,
	},
	"docker-compose": {
		"darwin": `Docker Compose is included with Docker Desktop for Mac.
If you need to install it separately:
1. Install using Homebrew:
   brew install docker-compose`,
		"linux": `Install Docker Compose:
1. Download the current stable release:
   sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
2. Apply executable permissions:
   sudo chmod +x /usr/local/bin/docker-compose`,
	},
	"helm": {
		"darwin": `Install Helm:
1. Install using Homebrew:
   brew install helm`,
		"linux": `Install Helm:
1. Download the installation script:
   curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
2. Make the script executable:
   chmod 700 get_helm.sh
3. Run the script:
   ./get_helm.sh`,
	},
}

// checkCommandAvailability checks if a command is available and returns installation instructions if not
func checkCommandAvailability(cmd string) error {
	_, err := exec.LookPath(cmd)
	if err != nil {
		platform := runtime.GOOS
		instructions, ok := installInstructions[cmd][platform]
		if !ok {
			instructions = fmt.Sprintf("Please install %s for your platform. Visit https://www.%s.com for more information.", cmd, cmd)
		}
		return fmt.Errorf("%s is not installed on your system.\n\nInstallation instructions:\n%s", cmd, instructions)
	}
	return nil
}

func verbose(f string, a ...interface{}) {
	v("DECPU:"+f+"\r\n", a...)
}

func flags() {
	flag.Parse()
	if *dump && *debug {
		log.Fatalf("You can only set either dump OR debug")
	}
	if *debug {
		v = log.Printf
		client.SetVerbose(verbose)
		ds.Verbose(verbose)
	}
	if *dump {
		var err error
		dumpWriter, err = os.CreateTemp("", "cpu")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Logging to %s", dumpWriter.Name())
		*dbg9p = true
		ulog.Log = log.New(dumpWriter, "", log.Ltime|log.Lmicroseconds)
		v = ulog.Log.Printf
	}
}

// getKeyFile picks a keyfile if none has been set.
// It will use sshconfig, else use a default.
func getKeyFile(host, kf string) string {
	verbose("getKeyFile for %q", kf)
	if len(kf) == 0 {
		kf = config.Get(host, "IdentityFile")
		verbose("key file from config is %q", kf)
		if len(kf) == 0 {
			kf = defaultKeyFile
		}
	}
	// The kf will always be non-zero at this point.
	if strings.HasPrefix(kf, "~") {
		kf = filepath.Join(os.Getenv("HOME"), kf[1:])
	}
	verbose("getKeyFile returns %q", kf)
	// this is a tad annoying, but the config package doesn't handle ~.
	return kf
}

// getHostName reads the host name from the config file,
// if needed. If it is not found, the host name is returned.
func getHostName(host string) string {
	h := config.Get(host, "HostName")
	if len(h) != 0 {
		host = h
	}
	return host
}

// getPort gets a port.
// The rules here are messy, since config.Get will return "22" if
// there is no entry in .ssh/config. 22 is not allowed. So in the case
// of "22", convert to defaultPort
func getPort(host, port string) string {
	p := port
	verbose("getPort(%q, %q)", host, port)
	if len(port) == 0 {
		if cp := config.Get(host, "Port"); len(cp) != 0 {
			verbose("config.Get(%q,%q): %q", host, port, cp)
			p = cp
		}
	}
	if len(p) == 0 || p == "22" {
		p = defaultPort
		verbose("getPort: return default %q", p)
	}
	verbose("returns %q", p)
	return p
}

// TODO: we've been tryinmg to figure out the right way to do usage for years.
// If this is a good way, it belongs in the uroot package.
func usage() {
	var b bytes.Buffer
	flag.CommandLine.SetOutput(&b)
	flag.PrintDefaults()
	log.Fatalf(`Usage: decpu [options] host [shell command]

Variants:
  decker    - Execute docker commands on remote system using local Docker socket
  decompose - Execute docker-compose commands on remote system using local compose file
  delm      - Execute helm commands on remote system using local Helm charts

Examples:
  decker host ps                    # List containers on remote using local Docker
  decompose host up                 # Start services using local docker-compose.yml
  delm host install ./chart         # Install Helm chart from local directory

Options:
%v`, b.String())
}

func newCPU(host, port string, extraEnv []string, args ...string) error {
	// note that 9P is enabled if namespace is not empty OR if ninep is true
	c := client.Command(host, args...)
	if err := c.SetOptions(
		client.WithPrivateKeyFile(*keyFile),
		client.WithHostKeyFile(*hostKeyFile),
		client.WithPort(port),
		client.WithRoot(*root),
		client.WithNameSpace(*namespace),
		client.With9P(*ninep),
		client.WithFSTab(*fstab),
		client.WithNetwork(*network),
		client.WithTimeout(*timeout9P)); err != nil {
		log.Fatal(err)
	}

	// Add environment variables from the host
	c.Env = append(os.Environ(), extraEnv...)

	// For special variants, ensure necessary files are mounted
	execName := filepath.Base(os.Args[0])
	switch execName {
	case "decker":
		// Ensure Docker socket is mounted
		if err := ensureDockerSocket(); err != nil {
			return fmt.Errorf("failed to setup Docker socket: %v", err)
		}
	case "decompose":
		// Ensure docker-compose.yml is mounted
		if err := ensureComposeFile(); err != nil {
			return fmt.Errorf("failed to setup docker-compose file: %v", err)
		}
	case "delm":
		// Ensure Helm charts are mounted if specified
		if *helmCharts != "" {
			if err := ensureHelmCharts(); err != nil {
				return fmt.Errorf("failed to setup Helm charts: %v", err)
			}
		}
	}

	if err := c.Dial(); err != nil {
		return fmt.Errorf("Dial: %v", err)
	}
	verbose("CPU:start")
	if err := c.Start(); err != nil {
		return fmt.Errorf("Start: %v", err)
	}
	verbose("CPU:wait")
	if err := c.Wait(); err != nil {
		log.Printf("Wait: %v", err)
	}
	verbose("CPU:close")
	err := c.Close()
	verbose("CPU:close done")
	return err
}

// Helper functions for special variants
func ensureDockerSocket() error {
	// Check if Docker socket exists
	if _, err := os.Stat(*dockerSocket); err != nil {
		return fmt.Errorf("Docker socket not found at %s: %v", *dockerSocket, err)
	}
	return nil
}

func ensureComposeFile() error {
	// Check if docker-compose.yml exists
	if _, err := os.Stat(*composeFile); err != nil {
		return fmt.Errorf("docker-compose.yml not found at %s: %v", *composeFile, err)
	}
	return nil
}

func ensureHelmCharts() error {
	// Check if Helm charts directory exists
	if _, err := os.Stat(*helmCharts); err != nil {
		return fmt.Errorf("Helm charts directory not found at %s: %v", *helmCharts, err)
	}
	return nil
}

func main() {
	flags()
	args := flag.Args()
	host := ds.Default
	port := *sp
	a := []string{}

	// Get the executable name to determine which variant we're running
	execName := filepath.Base(os.Args[0])
	var cmdPrefix string
	var extraEnv []string
	switch execName {
	case "decker":
		cmdPrefix = "docker"
		// Check if Docker is installed
		if err := checkCommandAvailability("docker"); err != nil {
			log.Fatal(err)
		}
		// Mount Docker socket from host
		extraEnv = append(extraEnv, "DOCKER_HOST=unix:///tmp/cpu"+*dockerSocket)
	case "decompose":
		cmdPrefix = "docker-compose"
		// Check if Docker Compose is installed
		if err := checkCommandAvailability("docker-compose"); err != nil {
			log.Fatal(err)
		}
		// Mount docker-compose.yml from host
		extraEnv = append(extraEnv, "COMPOSE_FILE=/tmp/cpu"+*composeFile)
	case "delm":
		cmdPrefix = "helm"
		// Check if Helm is installed
		if err := checkCommandAvailability("helm"); err != nil {
			log.Fatal(err)
		}
		if *helmCharts != "" {
			// Mount Helm charts from host
			extraEnv = append(extraEnv, "HELM_CHARTS=/tmp/cpu"+*helmCharts)
		}
	default:
		cmdPrefix = ""
	}

	if len(args) > 0 {
		host = args[0]
		a = args[1:]
	}
	if host == "." {
		host = ds.Default
	}
	if len(a) == 0 {
		if *numCPUs > 1 {
			log.Fatal("Interactive access with more than one CPU is not supported (yet)")
		}
		shellEnv := os.Getenv("SHELL")
		if len(shellEnv) > 0 {
			a = []string{shellEnv}
		} else {
			a = []string{"/bin/sh"}
		}
	}

	// If we're running one of the special variants, prepend the appropriate command
	if cmdPrefix != "" {
		a = append([]string{cmdPrefix}, a...)
	}

	// Try to parse it as a dnssd: path.
	// If that fails, we will run as though
	// it were just a host name.
	dq, err := ds.Parse(host)

	type cpu struct {
		host, port string
	}
	var cpus []cpu

	if err == nil {
		c, err := ds.Lookup(dq, *numCPUs)
		if err != nil {
			log.Printf("%v", err)
		}
		for _, e := range c {
			cpus = append(cpus, cpu{host: e.Entry.IPs[0].String(), port: strconv.Itoa(e.Entry.Port)})
		}
	} else {
		cpus = append(cpus, cpu{host: host, port: port})
	}

	verbose("Running as client, to host %q, args %q", host, a)

	var wg sync.WaitGroup
	for _, cpu := range cpus {
		wg.Add(1)
		*keyFile = getKeyFile(cpu.host, *keyFile)
		port = getPort(cpu.host, cpu.port)
		hn := getHostName(cpu.host)

		verbose("cpu to %v:%v", hn, port)
		if err := newCPU(hn, port, extraEnv, a...); err != nil {
			e := 1
			log.Printf("SSH error %s", err)
			sshErr := &ossh.ExitError{}
			if errors.As(err, &sshErr) {
				e = sshErr.ExitStatus()
			}
			log.Printf("%v", e)
		}
		wg.Done()
	}
	wg.Wait()
}
