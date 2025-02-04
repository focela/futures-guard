// Copyright (c) 2024 Focela Technologies. All rights reserved.
// Internal use only. Unauthorized use is prohibited.
// Contact: opensource@focela.com

// Package validate provides utility functions for data validation.
package validate

// InSlice checks if an element is in a slice.
func InSlice[K comparable](slice []K, key K) bool {
	for _, v := range slice {
		if v == key {
			return true
		}
	}
	return false
}
