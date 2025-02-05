// Copyright (c) 2024 Focela Technologies. All rights reserved.
// Internal use only. Unauthorized use is prohibited.
// Contact: opensource@focela.com

package service

import (
	"context"
	"omnis-core/internal/model"
)

type (
	ISysConfig interface {
		// GetLoadServeLog 获取本地服务日志配置
		GetLoadServeLog(ctx context.Context) (conf *model.ServeLogConfig, err error)
	}
)
