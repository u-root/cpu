// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ds

import (
	"context"
	"errors"
	"fmt"
	"github.com/brutella/dnssd"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"golang.org/x/exp/slices"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
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
	DsDefault  = "dnssd://?sort=tenants&sort=cpu.pcnt"
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
		// ignore sort criteria since they are optional
		if k == "sort" {
			continue
		}
		switch req[k][0][0] {
		case '<':
			fallthrough
		case '>':
			if len(req[k][0]) < 2 {
				v("error: poorly formed comparison in requirements")
				return false
			}
			reqval, err := strconv.ParseFloat(req[k][0][1:], 10)
			if err != nil {
				v("error: non-numeric comparison in requirement")
				return false
			}
			if len(src[k]) == 0 { // key not present, so requirement not met
				return false
			}
			val, err := strconv.ParseFloat(src[k], 10)
			if err != nil {
				v("error: non-numeric comparison in providing meta-data")
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
				v("error: poorly formed comparison in requirements")
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
		// remove prefix slash
		result.Type = u.Path[1:]
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

// --- sort and compare code ---

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
			v("Bad format in entry TXT")
			return false
		}
		n2, err := strconv.ParseFloat(c2.Text[key], 10)
		if err != nil {
			v("Bad format in entry TXT")
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
			v("Bad operator")
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
				v("dnssd: Poorly configured comparison in sort %s", element)
				return
			}
			element = element[1:]
		}
		ms.less = append(ms.less, dsGenSortTxt(element, operator))
	}
	sort.Sort(ms)
}

// --- end sort and compare code ---

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
	vmstat, err := mem.VirtualMemory()
	if err == nil {
		txtFlag["mem.avail"] = strconv.FormatUint(uint64(vmstat.Available), 10)
		txtFlag["mem.total"] = strconv.FormatUint(uint64(vmstat.Total), 10)
	}
	cpupcnt, err := cpu.Percent(0, false)
	if err == nil {
		txtFlag["cpu.pcnt"] = fmt.Sprintf("%.6f", float64(cpupcnt[0]))
	}
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
