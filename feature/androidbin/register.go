// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build (linux && !android) || (android && !cgo)

package androidbin

import (
	"github.com/sagernet/tailscale/feature"
	"github.com/sagernet/tailscale/net/netmon"
)

// The hook is not registered in GOOS=android cgo builds (the Android
// app), which register a Java-backed interface getter of their own.
func init() {
	if !feature.Register("androidbin") {
		return
	}
	netmon.HookInterfacesFallback.Set(fallbackInterfaces)
}
