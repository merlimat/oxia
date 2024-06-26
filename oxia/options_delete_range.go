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

package oxia

type deleteRangeOptions struct {
	baseOptions
}

// DeleteRangeOption represents an option for the [SyncClient.Delete] operation.
type DeleteRangeOption interface {
	applyDeleteRange(opts *deleteRangeOptions)
}

func newDeleteRangeOptions(opts []DeleteRangeOption) *deleteRangeOptions {
	deleteRangeOpts := &deleteRangeOptions{}
	for _, opt := range opts {
		opt.applyDeleteRange(deleteRangeOpts)
	}
	return deleteRangeOpts
}
