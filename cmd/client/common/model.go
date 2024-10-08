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

package common

import "time"

type OutputVersion struct {
	Key                string    `json:"key"`
	VersionId          int64     `json:"version_id"`
	CreatedTimestamp   time.Time `json:"created_timestamp"`
	ModifiedTimestamp  time.Time `json:"modified_timestamp"`
	ModificationsCount int64     `json:"modifications_count"`
	Ephemeral          bool      `json:"ephemeral"`
	SessionId          int64     `json:"session_id"`
	ClientIdentity     string    `json:"client_identity"`
}

type OutputError struct {
	Err string `json:"error,omitempty"`
}
