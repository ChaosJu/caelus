/*
 * Copyright (c) 2021 THL A29 Limited, a Tencent company.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 *
 * You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package resource

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/tencent/caelus/pkg/caelus/alarm"
	"github.com/tencent/caelus/pkg/caelus/checkpoint"
	"github.com/tencent/caelus/pkg/caelus/healthcheck/conflict"
	"github.com/tencent/caelus/pkg/caelus/metrics"
	"github.com/tencent/caelus/pkg/caelus/predict"
	"github.com/tencent/caelus/pkg/caelus/resource/yarn"
	"github.com/tencent/caelus/pkg/caelus/types"
	global "github.com/tencent/caelus/pkg/types"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
)

var _ clientInterface = (*yarnClient)(nil)

// OfflineYarnData describes options needed for yarn type
type OfflineYarnData struct {
	OfflineOnK8sCommonData
}

// yarnClient group node resource manager for yarn
type yarnClient struct {
	types.NodeResourceConfig
	predictor predict.Interface
	// checkpointManager
	checkpointManager *checkpoint.NodeResourceCheckpointManager
	scheduleDisabled  bool
	scheduleLock      sync.RWMutex
	// nodemanager operation client
	ginit               yarn.GInitInterface
	conflict            conflict.Manager
	metrics             *yarn.YarnMetrics
	lastCapIncTimestamp time.Time
	resourceHandlers    []yarn.ResourceAdapter
}

// NewYarnClient new an instance for yarn resource manager
func newYarnClient(config types.NodeResourceConfig, predictor predict.Interface,
	conflict conflict.Manager, offlineData interface{}) clientInterface {

	yarnData := offlineData.(*OfflineYarnData)
	yarn.AssignPodInformerValue(yarnData.PodInformer)
	diskManager := yarn.NewDiskManager(config.YarnConfig.Disks, yarnData.Client)
	ginit := yarn.NewGInit(config.DisableKillIfNormal, config.OnlyKillIfIncompressibleRes,
		config.YarnConfig, diskManager)
	nmPort, err := ginit.GetNMWebappPort()
	if err != nil {
		klog.Errorf("get nodemanager web port failed, using default value(%d): %v", err, nmPort)
	}

	// initialize resource adapter, which must be in order
	resourceHandlers := []yarn.ResourceAdapter{
		yarn.NewMinCompareAdapter(ginit),
		yarn.NewOverCommitAdapter(config.YarnConfig.CpuOverCommit, ginit),
		yarn.NewResourceReserveAdapter(config.YarnConfig.NMReserve, ginit),
		yarn.NewRoundOffAdapter(config.YarnConfig.ResourceRoundOff, ginit),
		yarn.NewDiskCpuAdapter(diskManager, ginit),
	}

	yc := &yarnClient{
		NodeResourceConfig: config,
		predictor:          predictor,
		scheduleDisabled:   false,
		ginit:              ginit,
		conflict:           conflict,
		metrics:            yarn.NewYarnMetrics(nmPort, ginit.WatchForMetricsPort()),
		checkpointManager:  yarnData.CheckpointManager,
		// the initialized timestamp ensure the first checking
		lastCapIncTimestamp: time.Now().Add(-config.YarnConfig.CapacityIncInterval.TimeDuration()),
		resourceHandlers:    resourceHandlers,
	}

	return yc
}

// Init waiting until nodemanager container is ready
func (y *yarnClient) Init() error {
	// waiting unitl nodemanager container is ready
	wait.PollImmediateInfinite(time.Duration(2*time.Second), y.checkNMContainerReady)
	return nil
}

// CheckPoint recover schedule state
func (y *yarnClient) CheckPoint() error {
	metrics.NodeScheduleDisabled(0)
	// for yarn, we need to notify master to enable schedule again if the check point is in disabled state,
	// so setting checkTime as false.
	checkScheduleDisable(y.checkpointManager, false, y.EnableOfflineSchedule, y.DisableOfflineSchedule)

	return nil
}

// module name
func (y *yarnClient) Name() string {
	return "ModuleResourceYarn"
}

// Run start kinds of resource handler thread
func (y *yarnClient) Run(stopCh <-chan struct{}) {
	y.metrics.Run(stopCh)
	for _, rh := range y.resourceHandlers {
		rh.Run(stopCh)
	}
}

// GetOfflineJobs return offline job list
func (y *yarnClient) GetOfflineJobs() ([]types.OfflineJobs, error) {
	return y.ginit.GetAllocatedJobs()
}

// DisableOfflineSchedule mark schedule disabled state
func (y *yarnClient) DisableOfflineSchedule() error {
	y.scheduleLock.Lock()
	defer y.scheduleLock.Unlock()

	if y.scheduleDisabled {
		klog.V(4).Infof("schedule is already closed")
		return nil
	}
	alarm.SendAlarm("schedule is closing")
	klog.V(2).Infof("schedule is closing")
	err := y.ginit.DisableSchedule()
	if err != nil {
		klog.Errorf("disable yarn schedule err: %v", err)
		return err
	}
	y.scheduleDisabled = true
	metrics.NodeScheduleDisabled(1)
	storeCheckpoint(y.checkpointManager, true)

	return nil
}

// EnableOfflineSchedule recover schedule enabled state
func (y *yarnClient) EnableOfflineSchedule() error {
	y.scheduleLock.Lock()
	defer y.scheduleLock.Unlock()

	if !y.scheduleDisabled {
		return nil
	}
	alarm.SendAlarm("schedule is opening")
	klog.V(2).Infof("schedule is opening")
	err := y.ginit.EnableSchedule()
	if err != nil {
		klog.Errorf("enable yarn schedule err: %v", err)
		return err
	}
	metrics.NodeScheduleDisabled(0)
	y.scheduleDisabled = false
	storeCheckpoint(y.checkpointManager, false)

	return nil
}

// KillOfflineJob kill yarn container based on conflicting resource
func (y *yarnClient) KillOfflineJob(conflictingResource v1.ResourceName) {
	y.ginit.KillContainer(conflictingResource)
}

// AdaptAndUpdateOfflineResource format and update resources
func (y *yarnClient) AdaptAndUpdateOfflineResource(offlineList v1.ResourceList,
	conflictingResources []string) error {
	restart, err := y.updateCapacity(offlineList, conflictingResources)
	if err != nil {
		klog.Errorf("update capacity err: %v", err)
	} else {
		if restart {
			klog.Infof("nodemanager capacity resource has changed, need to restart, stopping firstly")
			err = y.ginit.StopNodemanager()
			if err != nil {
				klog.Errorf("stop nodemanager err: %v", err)
			}
		}
	}

	// check if nodemanager process is running, and restart if not running
	y.startAndCheckNMProcess()

	return nil
}

// updateCapacity update resource to node, and may kill offline jobs if necessary
// restarted: if need to restart nodemanager process
func (y *yarnClient) updateCapacity(res v1.ResourceList,
	conflictingResources []string) (restarted bool, err error) {
	capChanged, capIncrease := y.beCapacity(res, len(conflictingResources) > 0)
	metrics.NodeResourceMetricsReset(res, metrics.NodeResourceTypeOfflineFormat)
	if !capChanged {
		klog.V(4).Infof("no need to change node resource capacity")
		return false, nil
	}
	klog.Infof("node resource will changed to %+v", res)
	klog.V(4).Infof("sync node resource, after nodemanager reserved: %+v", res)

	expectCap := &global.NMCapacity{
		Vcores:   res.Cpu().MilliValue() / types.CpuUnit,
		MemoryMB: res.Memory().Value() / types.MemUnit,
	}
	if capIncrease {
		if y.lastCapIncTimestamp.Add(y.YarnConfig.CapacityIncInterval.TimeDuration()).After(time.Now()) {
			klog.V(2).Infof("checking increasing capacity, while too frequently, nothing to do!")
			return false, nil
		}

		klog.Infof("increasing nodemanager capacity resource: %+v", expectCap)
		y.ginit.EnsureCapacity(expectCap, conflictingResources, false)
		y.lastCapIncTimestamp = time.Now()
	} else {
		y.ginit.EnsureCapacity(expectCap, conflictingResources, true)
	}
	// EnsureCapacity has already restart nodemanager
	return false, nil
}

// beCapacity format the resource quantity, and compare with the original capacity. The return values are:
// - capacityChanged: if the capacity has changed
// - capacityIncrease: if the capacity need to increase
func (y *yarnClient) beCapacity(res v1.ResourceList, conflicting bool) (capacityChanged bool, capacityIncrease bool) {
	var reachMin bool
	for _, rh := range y.resourceHandlers {
		reachMin = rh.ResourceAdapt(res)
		if reachMin {
			klog.Warningf("resource changing to min capacity: %+v", res)
			// already reach to min capacity, no need to check continuously
			break
		}
	}

	// should be update capacity or kill jobs when in conflicting state, even if reach to min capacity
	if conflicting {
		return true, false
	}

	cap, err := y.ginit.GetCapacity()
	if err != nil {
		klog.Errorf("get capacity from nodemanager err: %v", err)
		return true, true
	}

	// for min resource, just check if the original capacity equal to the min value, no need to check resource range
	if reachMin {
		minCap := y.ginit.GetMinCapacity()
		if cap.Vcores == minCap.Vcores || cap.Millcores == minCap.Millcores || cap.MemoryMB == minCap.MemoryMB {
			klog.V(3).Info("capacity has already in min capacity, no need to update")
			return false, false
		} else {
			klog.V(2).Infof("capacity(%+v) will be set min capacity(%+v)", cap, res)
			return true, false
		}
	}

	// check range resource
	cpu := res.Cpu().MilliValue()
	memMb := res.Memory().Value() / types.MemUnit
	rangeCPU, rangeMem := getRangeResource(y.YarnConfig.ResourceRange, cap)
	if math.Abs(float64(cpu-cap.Millcores)) <= rangeCPU && math.Abs(float64(memMb-cap.MemoryMB)) <= rangeMem {
		klog.V(4).Infof("new resource(mem:%d, cpu:%d) is in available range,"+
			"still using old values: %v", memMb, cpu, cap)
		return false, false
	}
	if cpu > cap.Millcores && memMb > cap.MemoryMB {
		return true, true
	}

	return true, false
}

// getRangeResource calculate range resources
func getRangeResource(resRange types.RangeResource, cap *global.NMCapacity) (rangeCPU, rangeMem float64) {
	if resRange.CPUMilli.Ratio+resRange.MemMB.Ratio == 0 {
		return 0, 0
	}

	rangeCPU = float64(cap.Millcores) * resRange.CPUMilli.Ratio
	if resRange.CPUMilli.Min != 0 && rangeCPU < resRange.CPUMilli.Min {
		rangeCPU = resRange.CPUMilli.Min
	}
	if rangeCPU > resRange.CPUMilli.Max {
		rangeCPU = resRange.CPUMilli.Max
	}

	rangeMem = float64(cap.MemoryMB) * resRange.MemMB.Ratio
	if resRange.MemMB.Min != 0 && rangeMem < resRange.MemMB.Min {
		rangeMem = resRange.MemMB.Min
	}
	if rangeMem > resRange.MemMB.Max {
		rangeMem = resRange.MemMB.Max
	}

	return rangeCPU, rangeMem
}

func (y *yarnClient) checkNMContainerReady() (bool, error) {
	_, err := y.ginit.GetStatus()
	if err != nil {
		klog.V(5).Infof("nodemanager container not ready")
		return false, nil
	} else {
		klog.V(5).Infof("nodemanager container ready")
		return true, nil
	}
}

func (y *yarnClient) checkNMProcessReady() (bool, error) {
	return y.ginit.GetStatus()
}

func (y *yarnClient) startAndCheckNMProcess() {
	waiting := time.Duration(10 * time.Second)

	// check if the nodemanager process is running
	running, err := y.checkNMProcessReady()
	if err != nil {
		klog.Errorf("nodemanager status err when checking: %v", err)
		return
	}
	if !running {
		klog.Infof("nodemanager is not running, try restarting")
		err = y.ginit.StartNodemanager()
		if err != nil {
			klog.Errorf("start nodemanager err when checking: %v", err)
			return
		} else {
			klog.Infof("nodemanager restart successfully, check again after %v", waiting)
			time.Sleep(waiting)
			running, err := y.checkNMProcessReady()
			if err != nil {
				klog.Errorf("nodemanager status check err after %v: %v", waiting, err)
				return
			}
			if !running {
				msg := fmt.Sprintf("nodemanager restart successfully, while not running after %v", waiting)
				klog.Errorf(msg)
				alarm.SendAlarm(msg)
			}
		}
	}

	return
}

// Describe implement the prometheus interface
func (y *yarnClient) Describe(ch chan<- *prometheus.Desc) {
	y.metrics.Describe(ch)
}

// Collect implement the  prometheus interface
func (y *yarnClient) Collect(ch chan<- prometheus.Metric) {
	y.metrics.Collect(ch)
}
