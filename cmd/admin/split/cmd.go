// Copyright 2023-2025 The Oxia Authors
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

package split

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/oxia-db/oxia/cmd/admin/commons"
	"github.com/oxia-db/oxia/oxia"
)

var (
	namespace string
	shardId   int64
)

var Cmd = &cobra.Command{
	Use:          "split",
	Short:        "Split a shard into two child shards",
	Long:         `Split a shard into two child shards by dividing its hash range at the midpoint`,
	Args:         cobra.ExactArgs(0),
	RunE:         exec,
	SilenceUsage: true,
}

func init() {
	Cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Namespace of the shard to split")
	Cmd.Flags().Int64VarP(&shardId, "shard", "s", 0, "Shard ID to split")
	_ = Cmd.MarkFlagRequired("shard")
}

func exec(cmd *cobra.Command, _ []string) error {
	client, err := commons.AdminConfig.NewAdminClient()
	if err != nil {
		return err
	}
	defer func(client oxia.AdminClient) {
		_ = client.Close()
	}(client)

	result := client.SplitShard(namespace, shardId)
	if result.Error != nil {
		return result.Error
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Shard %d split successfully into child shards:\n", shardId)
	fmt.Fprintf(cmd.OutOrStdout(), "  Child Low:  %d\n", result.ChildShardLow)
	fmt.Fprintf(cmd.OutOrStdout(), "  Child High: %d\n", result.ChildShardHigh)
	return nil
}
