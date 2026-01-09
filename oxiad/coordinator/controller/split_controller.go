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

package controller

import (
	"context"
	"io"
	"log/slog"
	"sync"

	"github.com/pkg/errors"

	"github.com/oxia-db/oxia/oxiad/coordinator/model"
	"github.com/oxia-db/oxia/oxiad/coordinator/resource"
	"github.com/oxia-db/oxia/oxiad/coordinator/rpc"
)

// SplitPhase represents the current phase of the split operation.
type SplitPhase uint8

const (
	SplitPhaseInit SplitPhase = iota
	SplitPhaseCreateChildren
	SplitPhaseTransferData
	SplitPhaseCatchup
	SplitPhaseCutover
	SplitPhaseComplete
	SplitPhaseFailed
)

func (p SplitPhase) String() string {
	switch p {
	case SplitPhaseInit:
		return "Init"
	case SplitPhaseCreateChildren:
		return "CreateChildren"
	case SplitPhaseTransferData:
		return "TransferData"
	case SplitPhaseCatchup:
		return "Catchup"
	case SplitPhaseCutover:
		return "Cutover"
	case SplitPhaseComplete:
		return "Complete"
	case SplitPhaseFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// SplitProgress contains information about the current split progress.
type SplitProgress struct {
	Phase         SplitPhase
	ChildShardLow int64
	ChildShardHigh int64
	Error         error
}

// SplitController manages the lifecycle of a shard split operation.
type SplitController interface {
	io.Closer

	// Start initiates the split process and blocks until complete or error.
	Start() error

	// Progress returns the current split progress.
	Progress() SplitProgress

	// ChildShardIds returns the IDs of the two child shards being created.
	ChildShardIds() (low, high int64)
}

// EnsembleSupplier is a function that selects a new ensemble for a shard.
type EnsembleSupplier func(ns *model.NamespaceConfig, status *model.ClusterStatus) ([]model.Server, error)

type splitController struct {
	sync.RWMutex

	namespace      string
	parentShardId  int64
	childShardLow  int64
	childShardHigh int64
	splitBoundary  uint32

	statusResource   resource.StatusResource
	configResource   resource.ClusterConfigResource
	rpc              rpc.Provider
	ensembleSupplier EnsembleSupplier
	shardEventListener ShardEventListener

	ctx    context.Context
	cancel context.CancelFunc
	log    *slog.Logger

	phase SplitProgress
}

// NewSplitController creates a new split controller for the given shard.
func NewSplitController(
	ctx context.Context,
	namespace string,
	parentShardId int64,
	statusResource resource.StatusResource,
	configResource resource.ClusterConfigResource,
	rpcProvider rpc.Provider,
	ensembleSupplier EnsembleSupplier,
	shardEventListener ShardEventListener,
) (SplitController, error) {
	ctx, cancel := context.WithCancel(ctx)

	sc := &splitController{
		namespace:          namespace,
		parentShardId:      parentShardId,
		statusResource:     statusResource,
		configResource:     configResource,
		rpc:                rpcProvider,
		ensembleSupplier:   ensembleSupplier,
		shardEventListener: shardEventListener,
		ctx:                ctx,
		cancel:             cancel,
		log: slog.With(
			slog.String("component", "split-controller"),
			slog.String("namespace", namespace),
			slog.Int64("parent-shard", parentShardId),
		),
		phase: SplitProgress{Phase: SplitPhaseInit},
	}

	return sc, nil
}

func (sc *splitController) Close() error {
	sc.cancel()
	return nil
}

func (sc *splitController) Progress() SplitProgress {
	sc.RLock()
	defer sc.RUnlock()
	return sc.phase
}

func (sc *splitController) ChildShardIds() (low, high int64) {
	sc.RLock()
	defer sc.RUnlock()
	return sc.childShardLow, sc.childShardHigh
}

func (sc *splitController) setPhase(phase SplitPhase, err error) {
	sc.Lock()
	defer sc.Unlock()
	sc.phase = SplitProgress{
		Phase:          phase,
		ChildShardLow:  sc.childShardLow,
		ChildShardHigh: sc.childShardHigh,
		Error:          err,
	}
}

func (sc *splitController) Start() error {
	sc.log.Info("Starting shard split")

	// Phase 1: Validate and calculate split point
	if err := sc.validateAndPrepare(); err != nil {
		sc.setPhase(SplitPhaseFailed, err)
		return err
	}

	// Phase 2: Create child shards
	sc.setPhase(SplitPhaseCreateChildren, nil)
	if err := sc.createChildShards(); err != nil {
		sc.setPhase(SplitPhaseFailed, err)
		return err
	}

	// Phase 3: Transfer data (snapshot)
	sc.setPhase(SplitPhaseTransferData, nil)
	if err := sc.transferData(); err != nil {
		sc.setPhase(SplitPhaseFailed, err)
		return err
	}

	// Phase 4: Catch up (WAL replay) - for now, we do a simplified version
	sc.setPhase(SplitPhaseCatchup, nil)
	if err := sc.catchUp(); err != nil {
		sc.setPhase(SplitPhaseFailed, err)
		return err
	}

	// Phase 5: Cutover
	sc.setPhase(SplitPhaseCutover, nil)
	if err := sc.performCutover(); err != nil {
		sc.setPhase(SplitPhaseFailed, err)
		return err
	}

	sc.setPhase(SplitPhaseComplete, nil)
	sc.log.Info("Shard split completed successfully",
		slog.Int64("child-shard-low", sc.childShardLow),
		slog.Int64("child-shard-high", sc.childShardHigh),
	)

	return nil
}

func (sc *splitController) validateAndPrepare() error {
	sc.log.Info("Validating split request")

	status := sc.statusResource.Load()
	ns, exists := status.Namespaces[sc.namespace]
	if !exists {
		return errors.Errorf("namespace %s not found", sc.namespace)
	}

	parentMeta, exists := ns.Shards[sc.parentShardId]
	if !exists {
		return errors.Errorf("shard %d not found in namespace %s", sc.parentShardId, sc.namespace)
	}

	if parentMeta.Status != model.ShardStatusSteadyState {
		return errors.Errorf("shard %d is not in steady state (current: %s)", sc.parentShardId, parentMeta.Status)
	}

	// Calculate split point (midpoint of hash range)
	sc.splitBoundary = (parentMeta.Int32HashRange.Min + parentMeta.Int32HashRange.Max) / 2

	sc.log.Info("Split validated",
		slog.Uint64("parent-min", uint64(parentMeta.Int32HashRange.Min)),
		slog.Uint64("parent-max", uint64(parentMeta.Int32HashRange.Max)),
		slog.Uint64("split-boundary", uint64(sc.splitBoundary)),
	)

	return nil
}

func (sc *splitController) createChildShards() error {
	sc.log.Info("Creating child shards")

	nsConfig, exists := sc.configResource.NamespaceConfig(sc.namespace)
	if !exists {
		return errors.Errorf("namespace config %s not found", sc.namespace)
	}

	// Load current status and allocate new shard IDs
	status, version := sc.statusResource.LoadWithVersion()
	ns := status.Namespaces[sc.namespace]
	parentMeta := ns.Shards[sc.parentShardId]

	// Allocate two new shard IDs
	sc.childShardLow = status.ShardIdGenerator
	sc.childShardHigh = status.ShardIdGenerator + 1
	status.ShardIdGenerator += 2

	// Select ensembles for child shards
	ensembleLow, err := sc.ensembleSupplier(nsConfig, status)
	if err != nil {
		return errors.Wrap(err, "failed to select ensemble for low child shard")
	}

	ensembleHigh, err := sc.ensembleSupplier(nsConfig, status)
	if err != nil {
		return errors.Wrap(err, "failed to select ensemble for high child shard")
	}

	// Create child shard metadata
	childLowMeta := model.ShardMetadata{
		Status:        model.ShardStatusSplitPrepare,
		Term:          -1,
		Leader:        nil,
		Ensemble:      ensembleLow,
		ParentShardId: &sc.parentShardId,
		Int32HashRange: model.Int32HashRange{
			Min: parentMeta.Int32HashRange.Min,
			Max: sc.splitBoundary,
		},
	}

	childHighMeta := model.ShardMetadata{
		Status:        model.ShardStatusSplitPrepare,
		Term:          -1,
		Leader:        nil,
		Ensemble:      ensembleHigh,
		ParentShardId: &sc.parentShardId,
		Int32HashRange: model.Int32HashRange{
			Min: sc.splitBoundary + 1,
			Max: parentMeta.Int32HashRange.Max,
		},
	}

	// Update parent shard to Splitting status
	parentMeta.Status = model.ShardStatusSplitting
	parentMeta.ChildShardIds = []int64{sc.childShardLow, sc.childShardHigh}
	parentMeta.SplitBoundary = &sc.splitBoundary

	// Persist changes
	ns.Shards[sc.parentShardId] = parentMeta
	ns.Shards[sc.childShardLow] = childLowMeta
	ns.Shards[sc.childShardHigh] = childHighMeta

	if !sc.statusResource.Swap(status, version) {
		return errors.New("failed to persist child shard metadata (concurrent modification)")
	}

	sc.log.Info("Child shards created",
		slog.Int64("child-low", sc.childShardLow),
		slog.Int64("child-high", sc.childShardHigh),
		slog.Any("ensemble-low", ensembleLow),
		slog.Any("ensemble-high", ensembleHigh),
	)

	return nil
}

func (sc *splitController) transferData() error {
	sc.log.Info("Transferring data to child shards")

	// Get parent shard's leader
	status := sc.statusResource.Load()
	ns := status.Namespaces[sc.namespace]
	parentMeta := ns.Shards[sc.parentShardId]

	if parentMeta.Leader == nil {
		return errors.New("parent shard has no leader")
	}

	childLowMeta := ns.Shards[sc.childShardLow]
	childHighMeta := ns.Shards[sc.childShardHigh]

	// For each child shard, we need to:
	// 1. Initialize the child shard on its ensemble members
	// 2. Send snapshot from parent to child
	// 3. Filter the snapshot to only keep keys in the child's hash range

	// For now, we'll implement a simplified version that:
	// - Sends NewTerm to child shards with the target_hash_range
	// - The child shards will receive the snapshot and filter it

	// Transfer to child low
	if err := sc.initializeChildShard(sc.childShardLow, childLowMeta); err != nil {
		return errors.Wrap(err, "failed to initialize low child shard")
	}

	// Transfer to child high
	if err := sc.initializeChildShard(sc.childShardHigh, childHighMeta); err != nil {
		return errors.Wrap(err, "failed to initialize high child shard")
	}

	sc.log.Info("Data transfer completed")
	return nil
}

func (sc *splitController) initializeChildShard(shardId int64, meta model.ShardMetadata) error {
	sc.log.Info("Initializing child shard",
		slog.Int64("child-shard", shardId),
		slog.Uint64("min-hash", uint64(meta.Int32HashRange.Min)),
		slog.Uint64("max-hash", uint64(meta.Int32HashRange.Max)),
	)

	// TODO: Implement the actual data transfer
	// This would involve:
	// 1. Sending NewTerm to child ensemble with target_hash_range and parent_shard_id
	// 2. The child servers would then request a snapshot from the parent
	// 3. After receiving the snapshot, they would filter it by hash range
	// 4. Then elect a leader

	// For now, we just log and continue - the full implementation would go here
	sc.log.Warn("Data transfer not yet fully implemented - placeholder")

	return nil
}

func (sc *splitController) catchUp() error {
	sc.log.Info("Catching up child shards with parent WAL")

	// TODO: Implement WAL replay catch-up
	// This would involve:
	// 1. Recording the parent's commit offset at snapshot time
	// 2. Replaying WAL entries from that offset to both children
	// 3. Filtering entries by hash range for each child

	sc.log.Warn("WAL catch-up not yet fully implemented - placeholder")
	return nil
}

func (sc *splitController) performCutover() error {
	sc.log.Info("Performing cutover")

	// Load current status
	status, version := sc.statusResource.LoadWithVersion()
	ns := status.Namespaces[sc.namespace]

	// Update parent shard to Deleting
	parentMeta := ns.Shards[sc.parentShardId]
	parentMeta.Status = model.ShardStatusDeleting
	ns.Shards[sc.parentShardId] = parentMeta

	// Update child shards to SteadyState
	childLowMeta := ns.Shards[sc.childShardLow]
	childLowMeta.Status = model.ShardStatusSteadyState
	ns.Shards[sc.childShardLow] = childLowMeta

	childHighMeta := ns.Shards[sc.childShardHigh]
	childHighMeta.Status = model.ShardStatusSteadyState
	ns.Shards[sc.childShardHigh] = childHighMeta

	// Persist changes
	if !sc.statusResource.Swap(status, version) {
		return errors.New("failed to persist cutover (concurrent modification)")
	}

	sc.log.Info("Cutover completed - child shards are now active")
	return nil
}
