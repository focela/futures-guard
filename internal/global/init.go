// Copyright (c) 2024 Focela Technologies. All rights reserved.
// Internal use only. Unauthorized use is prohibited.
// Contact: opensource@focela.com

// Package global handles the initialization of system-wide settings.
package global

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/gmode"

	"omnis-core/pkg/validate"
)

// Init initializes global settings.
func Init(ctx context.Context) {
	SetGFMode(ctx)
}

// SetGFMode configures the running mode of the GF framework.
// It retrieves the mode from the configuration and validates it before setting.
func SetGFMode(ctx context.Context) {
	mode := g.Cfg().MustGet(ctx, "system.mode").String()
	if len(mode) == 0 {
		mode = gmode.NOT_SET
	}

	validModes := []string{gmode.DEVELOP, gmode.TESTING, gmode.STAGING, gmode.PRODUCT}

	if validate.InSlice(validModes, mode) {
		gmode.Set(mode)
	}
}
