// Copyright 2023 StreamNative, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kv

import (
	"bytes"
)

var separator = []byte{'/'}

func compareWithSlash(a, b []byte) int {
	aCount := bytes.Count(a, separator)
	bCount := bytes.Count(b, separator)

	switch {
	case aCount < bCount:
		return -1
	case aCount > bCount:
		return +1
	default:
		return bytes.Compare(a, b)
	}
}
