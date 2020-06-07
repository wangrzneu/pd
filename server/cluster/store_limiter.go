// Copyright 2019 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License

package cluster

import (
	"github.com/pingcap/pd/v4/server/core"
	"sync"

	"github.com/pingcap/kvproto/pkg/pdpb"
	"github.com/pingcap/log"
	"github.com/pingcap/pd/v4/server/schedule"
	"github.com/pingcap/pd/v4/server/schedule/storelimit"
	"go.uber.org/zap"
)

// StoreLimiter adjust the store limit dynamically
type StoreLimiter struct {
	m       sync.RWMutex
	oc      *schedule.OperatorController
	scene   map[core.Engine]map[storelimit.Type]*storelimit.Scene
	state   *State
	current LoadState
}

// NewStoreLimiter builds a store limiter object using the operator controller
func NewStoreLimiter(c *schedule.OperatorController) *StoreLimiter {
	defaultScene := map[core.Engine]map[storelimit.Type]*storelimit.Scene{
		core.Unspecified: {
			storelimit.RegionAdd:    storelimit.DefaultScene(storelimit.RegionAdd),
			storelimit.RegionRemove: storelimit.DefaultScene(storelimit.RegionRemove),
		},
	}

	return &StoreLimiter{
		oc:      c,
		state:   NewState(),
		scene:   defaultScene,
		current: LoadStateNone,
	}
}

// Collect the store statistics and update the cluster state
func (s *StoreLimiter) Collect(stats *pdpb.StoreStats) {
	s.m.Lock()
	defer s.m.Unlock()

	log.Debug("collected statistics", zap.Reflect("stats", stats))
	s.state.Collect((*StatEntry)(stats))

	state := s.state.State()
	s.oc.SetAllStoresLimitAuto(state)
	collectClusterStateCurrent(state)
}

func collectClusterStateCurrent(state LoadState) {
	for i := LoadStateNone; i <= LoadStateHigh; i++ {
		if i == state {
			clusterStateCurrent.WithLabelValues(state.String()).Set(1)
			continue
		}
		clusterStateCurrent.WithLabelValues(i.String()).Set(0)
	}
}

// CalculateRate calculates the store limit rate according to limit type, store engine and load state
func (s *StoreLimiter) CalculateRate(limitType storelimit.Type, engine core.Engine, state LoadState) float64 {
	rate := float64(0)
	if s.scene[engine] != nil {
		switch state {
		case LoadStateIdle:
			rate = float64(s.scene[engine][limitType].Idle) / schedule.StoreBalanceBaseTime
		case LoadStateLow:
			rate = float64(s.scene[engine][limitType].Low) / schedule.StoreBalanceBaseTime
		case LoadStateNormal:
			rate = float64(s.scene[engine][limitType].Normal) / schedule.StoreBalanceBaseTime
		case LoadStateHigh:
			rate = float64(s.scene[engine][limitType].High) / schedule.StoreBalanceBaseTime
		}
	}
	return rate
}

// ReplaceStoreLimitScene replaces the store limit values for different scenes
func (s *StoreLimiter) ReplaceStoreLimitScene(scene *storelimit.Scene, limitType storelimit.Type, engine core.Engine, ) {
	s.m.Lock()
	defer s.m.Unlock()
	if s.scene == nil {
		s.scene = make(map[core.Engine]map[storelimit.Type]*storelimit.Scene)
	}
	s.scene[engine][limitType] = scene
}

// StoreLimitScene returns the current limit for different scenes
func (s *StoreLimiter) StoreLimitScene(limitType storelimit.Type, engine core.Engine) *storelimit.Scene {
	s.m.RLock()
	defer s.m.RUnlock()
	return s.scene[engine][limitType]
}
