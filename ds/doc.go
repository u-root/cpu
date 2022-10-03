// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Decentralized Services (aka ds)
// Inspired by http://man.cat-v.org/inferno/8/cs
//
// This package provides an opinionated DNS-SD for cpu and cpud
//
// Beyond basic service resolution, it provides and uses meta-data relating to
// the current configuration and state of the system in the DNS-SD TXT session
// which can be used to help select appropriate endpoint based on user specified
// (or sensible default) criteria.
//

package ds
