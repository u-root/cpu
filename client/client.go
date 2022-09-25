// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brutella/dnssd"
	"github.com/hashicorp/go-multierror"
	"github.com/mdlayher/vsock"
	"github.com/u-root/u-root/pkg/termios"
	"golang.org/x/crypto/ssh"
	"golang.org/x/exp/slices"
)

const (
	// From setting up the forward to having the nonce written back to us,
	// we would like to default to 100ms. This is a lot, considering that at this point,
	// the sshd has forked a server for us and it's waiting to be
	// told what to do.
	defaultTimeOut = time.Duration(100 * time.Millisecond)

	// DefaultNameSpace is the default used if the user does not request
	// something else.
	DefaultNameSpace = "/lib:/lib64:/usr:/bin:/etc:/home"
)

// V allows debug printing.
var V = func(string, ...interface{}) {}

// Cmd is a cpu client.
// It implements as much of exec.Command as makes sense.
type Cmd struct {
	config  ssh.ClientConfig
	client  *ssh.Client
	session *ssh.Session
	// CPU-specific options.
	// As in exec.Command, these controls are exposed and can
	// be set directly.
	Host string
	// HostName as found in .ssh/config; set to Host if not found
	HostName       string
	Args           []string
	Root           string
	HostKeyFile    string
	PrivateKeyFile string
	Port           string
	Timeout        time.Duration
	Env            []string
	Stdin          io.WriteCloser
	Stdout         io.Reader
	Stderr         io.Reader
	Row            int
	Col            int
	hasTTY         bool // Set if we have a TTY
	// NameSpace is a string as defined in the cpu documentation.
	NameSpace string
	// FSTab is an fstab(5)-format string
	FSTab string
	// Ninep determines if client will run a 9P server
	Ninep bool

	TmpMnt string

	nonce   nonce
	network string // This is a variable but we expect it will always be tcp
	port9p  uint16 // port on which we serve 9p
	cmd     string // The command is built up, bit by bit, as we configure the client
	closers []func() error
}

// SetOptions sets various options into the Command.
func (c *Cmd) SetOptions(opts ...Set) error {
	for _, o := range opts {
		if err := o(c); err != nil {
			return err
		}
	}
	return nil
}

func SetVerbose(f func(string, ...interface{})) {
	V = f
}

// Command implements exec.Command. The required parameter is a host.
// The args arg args to $SHELL. If there are no args, then starting $SHELL
// is assumed.
func Command(host string, args ...string) *Cmd {
	var hasTTY bool
	if len(args) == 0 {
		shell, ok := os.LookupEnv("SHELL")
		// We've found in some cases SHELL is not set!
		if !ok {
			shell = "/bin/sh"
		}
		args = []string{shell}
	}

	col, row := 80, 40
	if w, err := termios.GetWinSize(0); err != nil {
		verbose("Can not get winsize: %v; assuming %dx%d and non-interactive", err, col, row)
	} else {
		hasTTY = true
		col, row = int(w.Col), int(w.Row)
	}

	return &Cmd{
		Host:     host,
		HostName: GetHostName(host),
		Args:     args,
		Port:     DefaultPort,
		Timeout:  defaultTimeOut,
		Row:      row,
		Col:      col,
		config: ssh.ClientConfig{
			User:            os.Getenv("USER"),
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
		hasTTY:  hasTTY,
		network: "tcp",
		// Safety first: if they want a namespace, they must say so
		Root: "",
		// The command, always, at least, starts with "cpu"
		// We ship this command because it does allow for
		// using non-cpud to start cpud in --remote mode.
		// We are kind of stuck with this default for now,
		// as the original cpu implementation requires it.
		// Also, there is the nagging concern that we're not
		// totally proper yet on the security issues
		// around letting users run arbitrary binaries.
		cmd: "cpud -remote",
	}
}

// Set is the type of function used to set options in SetOptions.
type Set func(*Cmd) error

// With9P enables the 9P2000 server in cpu.
// The server is by default disabled. Ninep is sticky; if set by,
// e.g., WithNameSpace, the Principle of Least Confusion argues
// that it should remain set. Hence, we || it with its current value.
func With9P(p9 bool) Set {
	return func(c *Cmd) error {
		c.Ninep = p9 || c.Ninep
		return nil
	}
}

// WithNameSpace sets the namespace to Cmd.There is no default: having some default
// violates the principle of least surprise for package users. If ns is non-empty
// the Ninep is forced on.
func WithNameSpace(ns string) Set {
	return func(c *Cmd) error {
		c.NameSpace = ns
		if len(ns) > 0 {
			c.Ninep = true
		}
		return nil
	}
}

// WithFSTab reads a file for the FSTab member.
func WithFSTab(fstab string) Set {
	return func(c *Cmd) error {
		if len(fstab) == 0 {
			return nil
		}
		b, err := ioutil.ReadFile(fstab)
		if err != nil {
			return fmt.Errorf("Reading fstab: %w", err)
		}
		c.FSTab = string(b)
		return nil
	}
}

// WithTempMount sets the private namespace mount point.
func WithTempMount(tmpMnt string) Set {
	return func(c *Cmd) error {
		c.TmpMnt = tmpMnt
		return nil
	}
}

// WithTimeout sets the 9p timeout.
func WithTimeout(timeout string) Set {
	return func(c *Cmd) error {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return err
		}

		c.Timeout = d
		return nil
	}
}

// WithPrivateKeyFile adds a private key file to a Cmd
func WithPrivateKeyFile(key string) Set {
	return func(c *Cmd) error {
		c.PrivateKeyFile = key
		return nil
	}
}

// WithHostKeyFile adds a host key to a Cmd
func WithHostKeyFile(key string) Set {
	return func(c *Cmd) error {
		c.HostKeyFile = key
		return nil
	}
}

// WithRoot adds a root to a Cmd
func WithRoot(root string) Set {
	return func(c *Cmd) error {
		c.Root = root
		return nil
	}
}

// WithCpudCommand sets the initial command to run on the
// remote side. This is extremely helpful when testing new
// implementations of cpud, of little use otherwise.
func WithCpudCommand(cmd string) Set {
	return func(c *Cmd) error {
		if len(cmd) > 0 {
			c.cmd = cmd
		}
		return nil
	}
}

// WithNetwork sets the network. This almost never needs
// to be set, save for vsock.
func WithNetwork(network string) Set {
	return func(c *Cmd) error {
		if len(network) > 0 {
			c.network = network
		}
		return nil
	}
}

// WithPort sets the port in the Cmd.
// It calls GetPort with the passed-in port
// before assigning it.
func WithPort(port string) Set {
	return func(c *Cmd) error {
		if len(port) == 0 {
			p, err := GetPort(c.HostName, c.Port)
			if err != nil {
				return err
			}
			port = p
		}

		c.Port = port
		return nil
	}

}

// It's a shame vsock is not in the net package (yet ... or ever?)
func vsockDial(host, port string) (net.Conn, string, error) {
	id, portid, err := vsockIdPort(host, port)
	V("vsock(%v, %v) = %v, %v, %v", host, port, id, portid, err)
	if err != nil {
		return nil, "", err
	}
	addr := fmt.Sprintf("%#x:%d", id, portid)
	conn, err := vsock.Dial(id, portid, nil)
	V("vsock id %#x port %d addr %#x conn %v err %v", id, port, addr, conn, err)
	return conn, addr, err

}

// check that dnssd response has all required attributes
func ds_required(src map[string]string, req map[string][]string) bool {
	for k, _ := range req {
		// ignore sort criteria since they are optional
		if k == "sort" {
			continue
		}
		switch req[k][0][0] {
		case '<':
			fallthrough
		case '>':
			if len(req[k][0]) < 2 {
				V("error: poorly formed comparison in requirements")
				return false
			}
			reqval, err := strconv.ParseFloat(req[k][0][1:], 10)
			if err != nil {
				V("error: non-numeric comparison in requirement")
				return false
			}
			if len(src[k]) == 0 { // key not present, so requirement not met
				return false
			}
			val, err := strconv.ParseFloat(src[k], 10)
			if err != nil {
				V("error: non-numeric comparison in providing meta-data")
				return false
			}
			switch req[k][0][0] {
			case '<':
				if val > reqval {
					return false
				}
			case '>':
				if val < reqval {
					return false
				}
			}
		case '!':
			if len(req[k][0]) < 2 {
				V("error: poorly formed comparison in requirements")
				return false
			}
			if req[k][0][1:] == src[k] {
				return false
			}
		default:
			if !slices.Contains(req[k], src[k]) {
				return false
			}
		}
	}
	return true
}

type dsQuery struct {
	Type   string
	Domain string
	Text   map[string][]string
}

const DsDefault = "dnssd:"

// parse DNS-SD URI to dnssd struct
// we could subtype BrowseEntry or Service, but why?
func DsParse(uri string) (dsQuery, error) {
	result := dsQuery{
		Type:   "_ncpu._tcp",
		Domain: "local",
	}

	u, err := url.Parse(uri)
	if err != nil {
		return result, fmt.Errorf("Trouble parsing url %s: %w", uri, err)
	}

	if u.Scheme != "dnssd" {
		return result, fmt.Errorf("Not a dns-sd URI")
	}

	// following dns-sd URI conventions from CUPS
	if u.Host != "" {
		result.Domain = u.Host
	}
	if u.Path != "" {
		result.Type = u.Path
	}

	result.Text = u.Query()

	if len(result.Text["arch"]) == 0 {
		result.Text["arch"] = []string{runtime.GOARCH}
	}

	if len(result.Text["os"]) == 0 {
		result.Text["os"] = []string{runtime.GOOS}
	}

	return result, nil
}

type dsLessFunc func(p1, p2 *dnssd.BrowseEntry) bool
type dsMultiSorter struct {
	entries []dnssd.BrowseEntry
	less    []dsLessFunc
}

func (ms *dsMultiSorter) Len() int {
	return len(ms.entries)
}

func (ms *dsMultiSorter) Swap(i, j int) {
	ms.entries[i], ms.entries[j] = ms.entries[j], ms.entries[i]
}

// Less is part of sort.Interface. It is implemented by looping along the
// less functions until it finds a comparison that discriminates between
// the two items (one is less than the other). Note that it can call the
// less functions twice per call. We could change the functions to return
// -1, 0, 1 and reduce the number of calls for greater efficiency: an
// exercise for the reader.
func (ms *dsMultiSorter) Less(i, j int) bool {
	p, q := &ms.entries[i], &ms.entries[j]
	// Try all but the last comparison.
	var k int
	for k = 0; k < len(ms.less)-1; k++ {
		less := ms.less[k]
		switch {
		case less(p, q):
			// p < q, so we have a decision.
			return true
		case less(q, p):
			// p > q, so we have a decision.
			return false
		}
		// p == q; try the next comparison.
	}
	// All comparisons to here said "equal", so just return whatever
	// the final comparison reports.
	return ms.less[k](p, q)
}

// generate sort functions for dnssd BrowseEntry based on txt key
func dsGenSortTxt(key string, operator byte) dsLessFunc {
	return func(c1, c2 *dnssd.BrowseEntry) bool {
		switch operator {
		case '=': // key existence prioritizes entry
			if len(c1.Text[key]) > len(c2.Text[key]) {
				return true
			} else {
				return false
			}
		case '!': // key existence deprioritizes entry
			if len(c1.Text[key]) < len(c2.Text[key]) {
				return true
			} else {
				return false
			}
		}
		n1, err := strconv.ParseFloat(c1.Text[key], 10)
		if err != nil {
			V("Bad format in entry TXT")
			return false
		}
		n2, err := strconv.ParseFloat(c2.Text[key], 10)
		if err != nil {
			V("Bad format in entry TXT")
			return false
		}
		switch operator {
		case '<':
			if n1 < n2 {
				return true
			}
		case '>':
			if n1 > n2 {
				return true
			}
		default:
			V("Bad operator")
		}
		return false
	}
}

// perform numeric sort based on a particular key (assumes numeric values)
func dsSort(req map[string][]string, entries []dnssd.BrowseEntry) {
	if len(req["sort"]) == 0 {
		return
	}
	ms := &dsMultiSorter{
		entries: entries,
	}
	// generate a sort function list based on sort entry
	for _, element := range req["sort"] {
		var operator byte
		operator = '<' // default to use if no operator
		switch element[0] {
		case '<', '>', '=', '!':
			operator = element[0]
			if len(element) < 2 {
				V("dnssd: Poorly configured comparison in sort %s", element)
				return
			}
			element = element[1:]
		}
		ms.less = append(ms.less, dsGenSortTxt(element, operator))
	}
	sort.Sort(ms)
}

// lookup based on hostname, return resolved host, port, network, and error
// uri currently supported dnssd://domain/_service._network/instance?reqkey=reqvalue
// default for domain is local, first path element is _ncpu._tcp, and instance is wildcard
// can omit to underspecify, e.g. dnssd:?arch=arm64 to pick any arm64 cpu server
func DsLookup(query dsQuery) (string, string, error) {
	timeout := 1 * time.Second
	timeFormat := "15:04:05.000"

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	context.Canceled = errors.New("")
	context.DeadlineExceeded = errors.New("")
	defer cancel()

	service := fmt.Sprintf("%s.%s.", strings.Trim(query.Type, "."), strings.Trim(query.Domain, "."))

	V("Browsing for %s\n", service)

	respCh := make(chan *dnssd.BrowseEntry, 1)

	addFn := func(e dnssd.BrowseEntry) {
		V("%s	Add	%s	%s	%s	%s (%s)\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name, e.IPs)
		// check requirement
		V("Checking ", e.Text, query.Text)
		if ds_required(e.Text, query.Text) {
			respCh <- &e
		}
	}

	rmvFn := func(e dnssd.BrowseEntry) {
		V("%s	Rmv	%s	%s	%s	%s\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name)
		// we aren't maintaining cache so don't care?
	}

	go func() {
		if err := dnssd.LookupType(ctx, service, addFn, rmvFn); err != nil {
			fmt.Println(err)
		}
		respCh <- nil
	}()

	var responses []dnssd.BrowseEntry
	for {
		e := <-respCh
		if e == nil {
			break
		}
		responses = append(responses, *e)
	}

	cancel() // do we still do this?
	V("dnssd: Got %d responses", len(responses))

	if len(responses) == 0 {
		return "", "", fmt.Errorf("dnssd found no suitable service")
	}

	dsSort(query.Text, responses)

	e := responses[0]
	return e.Host, strconv.Itoa(e.Port), nil
}

// Dial implements ssh.Dial for cpu.
// Additionaly, if Cmd.Root is not "", it
// starts up a server for 9p requests.
func (c *Cmd) Dial() error {
	if err := c.UserKeyConfig(); err != nil {
		return err
	}
	// Sadly, no vsock in net package.
	var (
		conn net.Conn
		err  error
		addr string
	)

	switch c.network {
	case "vsock":
		conn, addr, err = vsockDial(c.HostName, c.Port)
	case "unix", "unixgram", "unixpacket":
		// There is not port on a unix domain socket.
		addr = c.network
		conn, err = net.Dial(c.network, c.Port)
	default:
		addr = net.JoinHostPort(c.HostName, c.Port)
		conn, err = net.Dial(c.network, addr)
	}
	V("connect: err %v", err)
	if err != nil {
		return err
	}
	sshconn, chans, reqs, err := ssh.NewClientConn(conn, addr, &c.config)
	if err != nil {
		return err
	}
	cl := ssh.NewClient(sshconn, chans, reqs)
	V("cpu:ssh.Dial(%s, %s, %v): (%v, %v)", c.network, addr, c.config, cl, err)
	if err != nil {
		return fmt.Errorf("Failed to dial: %v", err)
	}

	c.client = cl
	// Specifying a root is required for a remote namespace.
	if len(c.Root) == 0 {
		return nil
	}

	// Arrange port forwarding from remote ssh to our server.
	// Note: cl.Listen returns a TCP listener with network "tcp"
	// or variants. This lets us use a listen deadline.
	if c.Ninep {
		l, err := cl.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			// If ipv4 isn't available, try ipv6.  It's not enough
			// to use Listen("tcp", "localhost:0a)", since we (the
			// cpu client) might have v4 (which the runtime will
			// use if we say "localhost"), but the server (cpud)
			// might not.
			l, err = cl.Listen("tcp", "[::1]:0")
			if err != nil {
				return fmt.Errorf("cpu client listen for forwarded 9p port %v", err)
			}
		}
		V("ssh.listener %v", l.Addr().String())
		ap := strings.Split(l.Addr().String(), ":")
		if len(ap) == 0 {
			return fmt.Errorf("Can't find a port number in %v", l.Addr().String())
		}
		port9p, err := strconv.ParseUint(ap[len(ap)-1], 0, 16)
		if err != nil {
			return fmt.Errorf("Can't find a 16-bit port number in %v", l.Addr().String())
		}
		c.port9p = uint16(port9p)

		V("listener %T %v addr %v port %v", l, l, l.Addr().String(), port9p)

		nonce, err := generateNonce()
		if err != nil {
			log.Fatalf("Getting nonce: %v", err)
		}
		c.nonce = nonce
		c.Env = append(c.Env, "CPUNONCE="+nonce.String())
		if len(c.NameSpace) > 0 {
			c.Env = append(c.Env, "CPU_NAMESPACE="+c.NameSpace)
		}
		V("Set NONCE; set NAMESPACE to %q", "CPU_NAMESPACE="+c.NameSpace)
		c.Env = append(c.Env, "CPU_TMPMNT="+c.TmpMnt)
		V("Set CPU_TMPMNT to %q", "CPU_TMPMNT="+c.TmpMnt)
		go func(l net.Listener) {
			if err := c.srv(l); err != nil {
				log.Printf("9p server error: %v", err)
			}
		}(l)
	}
	if len(c.FSTab) > 0 {
		c.Env = append(c.Env, "CPU_FSTAB="+c.FSTab)
	}

	return nil
}

// Start implements exec.Start for CPU.
func (c *Cmd) Start() error {
	var err error
	if c.client == nil {
		return fmt.Errorf("Cmd has no client")
	}
	if c.session, err = c.client.NewSession(); err != nil {
		return err
	}
	// Set up terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	// Request pseudo terminal
	if c.hasTTY {
		V("c.session.RequestPty(\"ansi\", %v, %v, %#x", c.Row, c.Col, modes)
		if err := c.session.RequestPty("ansi", c.Row, c.Col, modes); err != nil {
			return fmt.Errorf("request for pseudo terminal failed: %v", err)
		}
	}

	c.closers = append(c.closers, func() error {
		if err := c.session.Close(); err != nil && err != io.EOF {
			return fmt.Errorf("closing session: %v", err)
		}
		return nil
	})

	if err := c.SetEnv(c.Env...); err != nil {
		return err
	}
	if c.Stdin, err = c.session.StdinPipe(); err != nil {
		return err
	}
	c.closers = append([]func() error{func() error {
		c.Stdin.Close()
		return nil
	}}, c.closers...)

	if c.Stdout, err = c.session.StdoutPipe(); err != nil {
		return err
	}
	if c.Stderr, err = c.session.StderrPipe(); err != nil {
		return err
	}

	// Unlike the cpu command source, which assumes an SSH-like stdin,
	// but very much like es/exec, users of Stdin in this package
	// will need to set the IO.
	// e.g.,
	// go c.SSHStdin(i, c.Stdin)
	// N.B.: if a 9p server was needed, it was started in Dial.

	cmd := c.cmd
	if c.port9p != 0 {
		cmd += fmt.Sprintf(" -port9p %v", c.port9p)
	}
	// The ABI for ssh.Start uses a string, not a []string
	// On the other end, it splits the string back up
	// as needed, claiming to do proper unquote handling.
	// This means we have to take care about quotes on
	// our side.
	//
	// Be careful here: you want to use
	// %v, not %q. %q will quote the string, and when
	// ssh server unpacks it, this will look like one arg.
	// This will manifest as weird problems when you
	// cpu host ls -l and such. The ls -l will end up being
	// a single arg. Why does this happen on cpu and not ssh?
	// cpu, unlike ssh, does not pass the arguments to a shell.
	// Unlike Plan 9 shells, Linux shells do gargantuan amounts
	// of file IO for each command, and it's a very noticeable
	// performance hit.
	// TODO:
	// Possibly the correct thing here is to loop over
	// c.Args and print each argument as %q.
	cmd += fmt.Sprintf(" %v", strings.Join(c.Args, " "))

	V("call session.Start(%s)", cmd)
	if err := c.session.Start(cmd); err != nil {
		return fmt.Errorf("Failed to run %v: %v", c, err.Error())
	}
	if c.hasTTY {
		V("Setup interactive input")
		if err := c.SetupInteractive(); err != nil {
			return err
		}
		go c.TTYIn(c.session, c.Stdin, os.Stdin)
	} else {
		go func() {
			if _, err := io.Copy(c.Stdin, os.Stdin); err != nil && !errors.Is(err, io.EOF) {
				log.Printf("copying stdin: %v", err)
			}
			if err := c.Stdin.Close(); err != nil {
				log.Printf("Closing stdin: %v", err)
			}
		}()
	}
	go func() {
		if _, err := io.Copy(os.Stdout, c.Stdout); err != nil && !errors.Is(err, io.EOF) {
			log.Printf("copying stdout: %v", err)
		}
	}()
	go func() {
		if _, err := io.Copy(os.Stderr, c.Stderr); err != nil && !errors.Is(err, io.EOF) {
			log.Printf("copying stderr: %v", err)
		}
	}()

	return nil
}

// Wait waits for a Cmd to finish.
func (c *Cmd) Wait() error {
	err := c.session.Wait()
	return err
}

// Run runs a command with Start, and waits for it to finish with Wait.
func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// TTYIn manages tty input for a cpu session.
// It exists mainly to deal with ~.
func (c *Cmd) TTYIn(s *ssh.Session, w io.WriteCloser, r io.Reader) {
	var newLine, tilde bool
	var t = []byte{'~'}
	var b [1]byte
	for {
		if _, err := r.Read(b[:]); err != nil {
			return
		}
		switch b[0] {
		default:
			newLine = false
			if tilde {
				if _, err := w.Write(t[:]); err != nil {
					return
				}
				tilde = false
			}
			if _, err := w.Write(b[:]); err != nil {
				return
			}
		case '\n', '\r':
			newLine = true
			if _, err := w.Write(b[:]); err != nil {
				return
			}
		case '~':
			if newLine {
				newLine = false
				tilde = true
				break
			}
			if _, err := w.Write(t[:]); err != nil {
				return
			}
		case '.':
			if tilde {
				s.Close()
				return
			}
			if _, err := w.Write(b[:]); err != nil {
				return
			}
		}
	}
}

// SetupInteractive sets up a cpu client for interactive access.
// It returns a function to be run when the session ends.
func (c *Cmd) SetupInteractive() error {
	t, err := termios.New()
	if err != nil {
		return err
	}
	r, err := t.Get()
	if err != nil {
		return err
	}
	if _, err = t.Raw(); err != nil {
		return err
	}
	c.closers = append(c.closers, func() error {
		if err := t.Set(r); err != nil {
			return err
		}
		return nil
	})

	return nil
}

// Close ends a cpu session, doing whatever is needed.
func (c *Cmd) Close() error {
	var err error
	for _, f := range c.closers {
		if e := f(); e != nil {
			err = multierror.Append(err, e)
		}
	}
	return err
}
