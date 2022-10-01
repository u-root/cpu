// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ds

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/brutella/dnssd"
	"golang.org/x/exp/slices"
	"golang.org/x/sys/unix" // TODO: doesn't build on OSX
)

// V allows debug printing.
var (
	v       = func(string, ...interface{}) {}
	cancel  = func() {}
	tenants = 0
	tenChan = make(chan int, 1)
)

// Simple form dns-sd query
type dsQuery struct {
	Type   string
	Domain string
	Text   map[string][]string
}

const (
	DsDefault  = "dnssd:"
	dsTimeout  = 1 * time.Second // query-timeout
	timeFormat = "15:04:05.000"
	dsUpdate   = 60 * time.Second // server meta-data refresh
)

// client relative code

// setup Verbose
func Verbose(f func(string, ...interface{})) {
	v = f
}

// check that dns-sd response has all required attributes
func required(src map[string]string, req map[string][]string) bool {
	for k, _ := range req {
		if !slices.Contains(req[k], src[k]) {
			return false
		}
	}
	return true
}

// parse DNS-SD URI to dnssd struct
// we could subtype BrowseEntry or Service, but why?
func Parse(uri string) (dsQuery, error) {
	result := dsQuery{
		Type:   "_ncpu._tcp",
		Domain: "local",
	}

	u, err := url.Parse(uri)
	if err != nil {
		return result, fmt.Errorf("Trouble parsing url %s: %w", uri, err)
	}

	if u.Scheme != "dnssd" {
		return result, fmt.Errorf("Not an dns-sd URI")
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

// lookup based on hostname, return resolved host, port, network, and error
// uri currently supported dnssd://domain/_service._network/instance?reqkey=reqvalue
// default for domain is local, first path element is _ncpu._tcp, and instance is wildcard
// can omit to underspecify, e.g. dnssd:?arch=arm64 to pick any arm64 cpu server
func Lookup(query dsQuery) (string, string, error) {
	var (
		err error
	)

	ctx, cancel := context.WithTimeout(context.Background(), dsTimeout)
	context.Canceled = errors.New("")
	context.DeadlineExceeded = errors.New("")
	defer cancel()

	service := fmt.Sprintf("%s.%s.", strings.Trim(query.Type, "."), strings.Trim(query.Domain, "."))

	v("Browsing for %s\n", service)

	respCh := make(chan *dnssd.BrowseEntry, 1)

	addFn := func(e dnssd.BrowseEntry) {
		v("%s	Add	%s	%s	%s	%s (%s)\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name, e.IPs)
		// check requirement
		v("Checking ", e.Text, query.Text)
		if required(e.Text, query.Text) {
			respCh <- &e
		}
	}

	rmvFn := func(e dnssd.BrowseEntry) {
		v("%s	Rmv	%s	%s	%s	%s\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name)
		// we aren't maintaining cache so don't care?
	}

	go func() {
		if err := dnssd.LookupType(ctx, service, addFn, rmvFn); err != nil {
			fmt.Println(err)
		}
		respCh <- nil
	}()

	e := <-respCh

	// cancel()
	if e == nil {
		return "", "", fmt.Errorf("dnssd found no suitable service")
	}

	if len(e.IPs) > 1 {
		v("WARNING: there was more than one option for address")
	}

	return e.IPs[0].String(), strconv.Itoa(e.Port), err
}

// Server components

// Parse DNS-SD key value string into Map w/sensible default for empty keys
func ParseKv(arg string) map[string]string {
	txt := make(map[string]string)
	if len(arg) == 0 {
		return txt
	}
	ss := strings.Split(arg, ",")
	for _, pair := range ss {
		z := strings.SplitN(pair, "=", 2)
		if len(z) > 1 {
			txt[z[0]] = z[1]
		} else {
			txt[z[0]] = "true"
		}
	}

	return txt
}

func Unregister() {
	v("stopping dns-sd server")
	cancel()
}

func DefaultInstance() string {
	hostname, err := os.Hostname()
	if err == nil {
		hostname += "-cpud"
	} else {
		hostname = "cpud"
	}

	return hostname
}

func UpdateSysInfo(txtFlag map[string]string) {
	var sysinfo unix.Sysinfo_t
	err := unix.Sysinfo(&sysinfo)

	if err != nil {
		v("Sysinfo call failed ", err)
		return
	}

	txtFlag["mem_avail"] = strconv.FormatUint(uint64(sysinfo.Freeram), 10)
	txtFlag["mem_total"] = strconv.FormatUint(uint64(sysinfo.Totalram), 10)
	txtFlag["mem_unit"] = strconv.FormatUint(uint64(sysinfo.Unit), 10)
	txtFlag["load1"] = strconv.FormatUint(uint64(sysinfo.Loads[0]), 10)
	txtFlag["load5"] = strconv.FormatUint(uint64(sysinfo.Loads[1]), 10)
	txtFlag["load15"] = strconv.FormatUint(uint64(sysinfo.Loads[2]), 10)
	txtFlag["load_ratio"] = fmt.Sprintf("%.6f", float64(sysinfo.Loads[1])/float64(runtime.NumCPU()))
	txtFlag["tenants"] = strconv.Itoa(tenants)

	v(" dsUpdateSysInfo ", txtFlag)
}

func DefaultTxt(txtFlag map[string]string) {
	if len(txtFlag["arch"]) == 0 {
		txtFlag["arch"] = runtime.GOARCH
	}

	if len(txtFlag["os"]) == 0 {
		txtFlag["os"] = runtime.GOOS
	}

	if len(txtFlag["cores"]) == 0 {
		txtFlag["cores"] = strconv.Itoa(runtime.NumCPU())
	}
}

// update tenant count by delta
func Tenant(delta int) {
	v("tenant delta %d", delta)
	tenChan <- delta
}

func Register(instanceFlag, domainFlag, serviceFlag, interfaceFlag string, portFlag int, txtFlag map[string]string) error {
	v("starting dns-sd server")

	timeFormat := "15:04:05.000"

	v("Advertising: %s.%s.%s.", strings.Trim(instanceFlag, "."), strings.Trim(serviceFlag, "."), strings.Trim(domainFlag, "."))

	ctx, ctxCancel := context.WithCancel(context.Background())
	cancel = ctxCancel

	resp, err := dnssd.NewResponder()
	if err != nil {
		return fmt.Errorf("dnssd newreponder fail: %w", err)
	}

	ifaces := []string{}
	if len(interfaceFlag) > 0 {
		ifaces = append(ifaces, interfaceFlag)
	}

	if len(instanceFlag) == 0 {
		instanceFlag = DefaultInstance()
	}

	DefaultTxt(txtFlag)
	UpdateSysInfo(txtFlag)

	cfg := dnssd.Config{
		Name:   instanceFlag,
		Type:   serviceFlag,
		Domain: domainFlag,
		Port:   portFlag,
		Ifaces: ifaces,
		Text:   txtFlag,
	}
	srv, err := dnssd.NewService(cfg)
	if err != nil {
		return fmt.Errorf("cpud: advertise: New service fail: %w", err)
	}

	go func() {
		time.Sleep(1 * time.Second)
		handle, err := resp.Add(srv)
		if err != nil {
			fmt.Println(err)
		} else {
			v("%s	Got a reply for service %s: Name now registered and active\n", time.Now().Format(timeFormat), handle.Service().ServiceInstanceName())
		}
		go func() {
			for {
				delta := <-tenChan
				tenants += delta
				UpdateSysInfo(txtFlag)
				handle.UpdateText(txtFlag, resp)
			}
		}()

		for {
			time.Sleep(dsUpdate)
			tenChan <- 0
		}
	}()

	go func() {
		err = resp.Respond(ctx)
		if err != nil {
			fmt.Println(err)
		} else {
			v("cpu dns-sd responder running exited")
		}
	}()

	return err
}
