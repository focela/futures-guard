// Copyright (c) 2024 Focela Technologies. All rights reserved.
// Internal use only. Unauthorized use is prohibited.
// Contact: opensource@focela.com

// Package main is the entry point of the application.
package main

import (
	"github.com/gogf/gf/v2/os/gctx"

	"omnis-core/internal/global"
)

// main initializes the application context and global settings.
func main() {
	ctx := gctx.GetInitCtx()
	global.Init(ctx)
}
