// Copyright (c) 2024 Focela Technologies. All rights reserved.
// Internal use only. Unauthorized use is prohibited.
// Contact: opensource@focela.com

// Package model defines core data structures.
package model

// ServeLogConfig defines service log settings.
type ServeLogConfig struct {
	Switch      bool     `json:"switch"`
	Queue       bool     `json:"queue"`
	LevelFormat []string `json:"levelFormat"`
}
