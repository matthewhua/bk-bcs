/*
 * Tencent is pleased to support the open source community by making Blueking Container Service available.
 * Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package scheduler

import (
	"net/http"
	"time"

	"bk-bcs/bcs-common/common/blog"
	commtypes "bk-bcs/bcs-common/common/types"
	"bk-bcs/bcs-mesos/bcs-scheduler/src/manager/sched/offer"
	"bk-bcs/bcs-mesos/bcs-scheduler/src/manager/sched/task"
	"bk-bcs/bcs-mesos/bcs-scheduler/src/mesosproto/mesos"
	"bk-bcs/bcs-mesos/bcs-scheduler/src/types"
	"bk-bcs/bcs-mesos/bcs-scheduler/src/util"
)

func (s *Scheduler) startBuildDaemonsets() {
	//start check and build daemonset
	//only master do the function
	s.stopDaemonset = make(chan struct{})
	for {
		time.Sleep(time.Second * 5)
		select {
		case <-s.stopDaemonset:
			blog.Warnf("stop check and build daemonset")
			return
		default:
			//ticker check and build daemonset
		}
		//fetch all daemonsets in cluster
		daemonsets, err := s.store.ListAllDaemonset()
		if err != nil {
			blog.Errorf("ListAllDaemonset failed: %s", err.Error())
			continue
		}
		for _, daemon := range daemonsets {
			switch daemon.Status {
			case types.Daemonset_Status_Deleting:
				s.deleteDaemonset(daemon)
			default:
				s.checkDaemonsetPod(daemon)
			}
		}
	}
}

//stop check and build daemonset
func (s *Scheduler) stopBuildDaemonset() {
	if s.stopDaemonset != nil {
		close(s.stopDaemonset)
	}
}

//checkDaemonsetPod check taskgroup status
//if some offers don't deploy daemonset, then build new taskgroup in the offer
func (s *Scheduler) checkDaemonsetPod(daemon *types.BcsDaemonset) {
	blog.V(3).Infof("start check daemonset(%s) pods, and build taskgroup on new offer", daemon.GetUuid())
	//get current all mesos offers
	offers := s.GetAllOffers()
	if len(offers) == 0 {
		blog.V(3).Infof("the cluster don't have any mesos-slave now")
		return
	}
	for _, inoffer := range offers {
		//get offer hostip
		hostIp, ok := offer.GetOfferIp(inoffer.Offer)
		//if offer don't have InnerIP attribute, then continue
		if !ok {
			blog.Warnf("launch daemonset(%s) offer(%s:%s) don't have attribute InnerIP",
				daemon.GetUuid(), inoffer.Offer.GetId().GetValue(), inoffer.Offer.GetHostname())
			continue
		}

		exist := s.checkofferWhetherBuildPod(daemon, hostIp)
		//if exist==true, then continue
		if exist {
			continue
		}
		blog.Infof("daemonset(%s) don't have taskgroup in agent(%s), then will launch", daemon.GetUuid(), hostIp)
		//the offer don't contain the damonset instance, then build new taskgroup on new pod
		s.doLaunchDaemonset(daemon, inoffer)
	}
	blog.V(3).Infof("check daemonset(%s) pods and build taskgroup on new offer done", daemon.GetUuid())
}

//check daemonset whether build taskgroup in offer(hostIp)
//if build taskgroup return true, then return false
func (s *Scheduler) checkofferWhetherBuildPod(daemon *types.BcsDaemonset, hostIp string) bool {
	//if daemonset don't have any pod, return false
	if len(daemon.Pods) == 0 {
		return false
	}

	for _, podId := range daemon.Pods {
		pod, err := s.store.FetchTaskGroup(podId.Name)
		if err != nil {
			blog.Errorf("check daemonset(%s:%s) whether build offer, fetch taskgroup(%s) failed: %s",
				daemon.NameSpace, daemon.Name, podId.Name, err.Error())
			continue
		}
		//if taskgroup failed or finished, then delete it
		if pod.Status == types.TASKGROUP_STATUS_FINISH || pod.Status == types.TASKGROUP_STATUS_FAIL {
			util.Lock.Lock(types.BcsDaemonset{}, daemon.GetUuid())
			defer util.Lock.UnLock(types.BcsDaemonset{}, daemon.GetUuid())

			indaemon, err := s.store.FetchDaemonset(daemon.NameSpace, daemon.Name)
			if err != nil {
				blog.Errorf("Fetch Daemonset(%s) failed: %s", daemon.GetUuid(), err.Error())
				continue
			}
			indaemon.Pods = s.DeleteDaemonsetTaskGroup(indaemon, pod)
			indaemon.Instances = uint64(len(indaemon.Pods))
			err = s.store.SaveDaemonset(indaemon)
			if err != nil {
				blog.Errorf("delete daemonset(%s) TaskGroup(%s), but SaveDaemonset error %s", daemon.GetUuid(), podId.Name, err.Error())
			} else {
				blog.Infof("daemonset(%s) TaskGroup(%s) status(%s), and delete it success", daemon.GetUuid(), podId.Name, pod.Status)
			}
			//if pod.AgentIp == hostIp, show the offer have builded the daemonset taskgroup
		} else if hostIp == pod.GetAgentIp() {
			blog.V(3).Infof("daemonset(%s) have taskgroup(%s) in agent(%s)", daemon.GetUuid(), podId.Name, hostIp)
			return true
		}
	}

	return false
}

func (s *Scheduler) doLaunchDaemonset(o *types.BcsDaemonset, outOffer *offer.Offer) {
	util.Lock.Lock(types.BcsDaemonset{}, o.GetUuid())
	defer util.Lock.UnLock(types.BcsDaemonset{}, o.GetUuid())

	daemon, err := s.store.FetchDaemonset(o.NameSpace, o.Name)
	if err != nil {
		blog.Errorf("launch daemonset(%s) taskgroup(%s) FetchDaemonset error %s", daemon.GetUuid(), err.Error())
		return
	}
	//when daemon.status==deleting, don't launch taskgroup anymore
	if daemon.Status == types.Daemonset_Status_Deleting {
		blog.Infof("Daemonset(%s) status %s, then don't need launch taskgroup", daemon.GetUuid(), daemon.Status)
		return
	}
	//get offer innerip、cpu、mem、dis
	offerIp, _ := offer.GetOfferIp(outOffer.Offer)
	offer := outOffer.Offer
	cpus, mem, disk := s.OfferedResources(offer)

	var taskGroupInfos []*mesos.TaskGroupInfo
	version, err := s.store.GetVersion(daemon.NameSpace, daemon.Name)
	if err != nil {
		blog.Errorf("launch daemonset(%s:%s) with offer(%s), but get version failed:%s",
			daemon.NameSpace, daemon.Name, offerIp, err.Error())
		return
	}
	resources := task.BuildResources(version.AllResource())
	var taskgroupId string
	//check offer resource whether fit launch daemonset
	if s.IsOfferResourceFitLaunch(version.AllResource(), outOffer) && s.IsOfferExtendedResourcesFitLaunch(version.GetExtendedResources(), outOffer) {
		//use the offer to build daemonset taskgroup
		//if others use the offer, the UseOffer will return false
		if !s.UseOffer(outOffer) {
			blog.Warnf("launch daemonset(%s) use offer(%s) failed", daemon.GetUuid(), offerIp)
			return
		}
		blog.Info("launch daemonset(%s.%s) with offer:%s||%s, cpu:%f, mem:%f, disk:%f",
			daemon.NameSpace, daemon.Name, offerIp, *(offer.Id.Value), cpus, mem, disk)
		instanceId := uint64(len(daemon.Pods))
		//create taskgroup base on version
		taskgroup, err := task.CreateTaskGroup(version, "", instanceId, s.ClusterId, "", s.store)
		if err != nil {
			blog.Errorf("launch daemonset(%s) create taskgroup err: %s", daemon.GetUuid(), err.Error())
			s.DeclineResource(offer.Id.Value)
			return
		}
		//save taskgroup
		err = s.store.SaveTaskGroup(taskgroup)
		if err != nil {
			blog.Errorf("launch daemonset(%s) save taskgroup(%s) err: %s", daemon.GetUuid(), taskgroup.ID, err.Error())
			s.DeclineResource(offer.Id.Value)
			return
		}
		//update daemonset information
		taskgroupId = taskgroup.ID
		daemon.Pods = append(daemon.Pods, &commtypes.BcsPodIndex{Name: taskgroupId})
		daemon.Instances = uint64(len(daemon.Pods))
		//create mesos taskgroup base inner taskgroup
		taskGroupInfo := task.CreateTaskGroupInfo(offer, version, resources, taskgroup)
		if taskGroupInfo == nil {
			blog.Errorf("launch daemonset(%s) build taskgroupinfo fail", daemon.GetUuid())
			s.DeleteDaemonsetTaskGroup(daemon, taskgroup)
			s.DeclineResource(offer.Id.Value)
			return
		}

		if err := s.store.SaveTaskGroup(taskgroup); err != nil {
			blog.Errorf("launch daemonset(%s) save taskgroup(%s) err: %s", daemon.GetUuid(), taskgroup.ID, err.Error())
			s.DeleteDaemonsetTaskGroup(daemon, taskgroup)
			s.DeclineResource(offer.Id.Value)
			return
		}
		taskGroupInfos = append(taskGroupInfos, taskGroupInfo)
		//lock agentsetting
		util.Lock.Lock(commtypes.BcsClusterAgentSetting{}, taskgroup.GetAgentIp())
		//update agentsettings taskgroup index info
		agentsetting, _ := s.store.FetchAgentSetting(taskgroup.GetAgentIp())
		if agentsetting != nil {
			agentsetting.Pods = append(agentsetting.Pods, taskgroup.ID)
			err := s.store.SaveAgentSetting(agentsetting)
			if err != nil {
				blog.Errorf("save agentsetting %s pods error %s", agentsetting.InnerIP, err.Error())
			}
		} else {
			blog.Errorf("fetch agentsetting %s Not Found", taskgroup.GetAgentIp())
		}
		util.Lock.UnLock(commtypes.BcsClusterAgentSetting{}, taskgroup.GetAgentIp())
	} else {
		blog.Warnf("launch daemonset(%s) with offer:%s||%s, cpu:%f, mem:%f, disk:%f not fit resources",
			daemon.GetUuid(), offerIp, *(offer.Id.Value), cpus, mem, disk)
		s.DeclineResource(offer.Id.Value)
		return
	}

	//launch taskgroup to mesos cluster
	resp, err := s.LaunchTaskGroups(offer, taskGroupInfos, version)
	if err != nil {
		blog.Errorf("launch daemonset(%s) taskgroup(%s) err:%s", daemon.GetUuid(), taskgroupId, err.Error())
		s.DeclineResource(offer.Id.Value)
		return
	}
	if resp != nil && resp.StatusCode != http.StatusAccepted {
		blog.Error("launch daemonset(%s) taskgroup(%s) resp status err code : %d", daemon.GetUuid(), taskgroupId, resp.StatusCode)
		s.DeclineResource(offer.Id.Value)
		return
	}

	//launch taskgroup success, and metrics
	reportScheduleTaskgroupMetrics(daemon.NameSpace, daemon.Name, taskgroupId, LaunchTaskgroupType, time.Now())
	//update daemonset info
	daemon.LastUpdateTime = time.Now().Unix()
	err = s.store.SaveDaemonset(daemon)
	if err != nil {
		blog.Errorf("launch daemonset(%s) taskgroup(%s) SaveDaemonset error %s", daemon.GetUuid(), taskgroupId, err.Error())
	}
	blog.Infof("launch daemonset(%s) taskgroup(%s) on agent(%s) success", daemon.GetUuid(), taskgroupId, offerIp)
	return
}

//check taskgroup't status, update daemonset status
func (s *Scheduler) updateDaemonsetStatus(namespace, name string) {
	daemon, err := s.store.FetchDaemonset(namespace, name)
	if err != nil {
		blog.Errorf("update daemonset(%s) status, but FetchDaemonset error %s", daemon.GetUuid(), err.Error())
		return
	}
	now := time.Now().Unix()
	updateTime := now - MAX_DATA_UPDATE_INTERVAL
	//when daemonset.status==deleting, not need change daemonset status
	if daemon.Status == types.Daemonset_Status_Deleting {
		blog.V(3).Infof("daemonset(%s) status %s, then not need change it", daemon.GetUuid(), daemon.Status)
		return
	}

	var nowStatus string
	var runningInstance uint64
	var failedInstance uint64
	var startingInstance uint64
	updated := false
	for _, podId := range daemon.Pods {
		pod, err := s.store.FetchTaskGroup(podId.Name)
		if err != nil {
			blog.Errorf("update daemonset(%s:%s) status, fetch taskgroup(%s) failed: %s",
				daemon.NameSpace, daemon.Name, podId.Name, err.Error())
			continue
		}

		//when any pod exit, then daemonset.status==abnormal
		if pod.Status == types.TASKGROUP_STATUS_FINISH || pod.Status == types.TASKGROUP_STATUS_FAIL ||
			pod.Status == types.TASKGROUP_STATUS_LOST {
			failedInstance += 1
		}
		if pod.Status == types.TASKGROUP_STATUS_RUNNING {
			runningInstance += 1
		}
		if pod.Status == types.TASKGROUP_STATUS_STAGING || pod.Status == types.TASKGROUP_STATUS_STARTING {
			startingInstance += 1
		}
	}
	//if some  taskgroup failed
	if failedInstance > 0 {
		blog.Infof("daemonset(%s) have failed(%d), running(%d), starting(%d) taskgroups", daemon.GetUuid(), failedInstance, runningInstance, startingInstance)
		nowStatus = types.Daemonset_Status_Abnormal
	} else if startingInstance > 0 {
		blog.Infof("daemonset(%s) have failed(%d), running(%d), starting(%d) taskgroups", daemon.GetUuid(), failedInstance, runningInstance, startingInstance)
		nowStatus = types.Daemonset_Status_Starting
	} else {
		blog.V(3).Infof("daemonset(%s) have failed(%d), running(%d), starting(%d) taskgroups", daemon.GetUuid(), failedInstance, runningInstance, startingInstance)
		nowStatus = types.Daemonset_Status_Running
	}

	//if daemonset information changed
	if nowStatus != daemon.Status {
		blog.Infof("update daemonset(%s) status from(%s)->to(%s)", daemon.GetUuid(), daemon.Status, nowStatus)
		daemon.LastStatus = daemon.Status
		daemon.Status = nowStatus
		updated = true
	}
	if daemon.RunningInstances != runningInstance {
		daemon.RunningInstances = runningInstance
		updated = true
	}

	//daemonset status not changed, then return
	if !updated && daemon.LastUpdateTime > updateTime {
		return
	}
	daemon.LastUpdateTime = now
	err = s.store.SaveDaemonset(daemon)
	if err != nil {
		blog.Errorf("update daemonset(%s) status, but SaveDaemonset failed: %s", daemon.GetUuid(), err.Error())
	} else {
		blog.V(3).Infof("update daemonset(%s) lastStatus(%s) Status(%s) success", daemon.GetUuid(), daemon.LastStatus, daemon.Status)
	}
}

//delete daemonset
func (s *Scheduler) deleteDaemonset(daemon *types.BcsDaemonset) {
	//lock
	util.Lock.Lock(types.BcsDaemonset{}, daemon.GetUuid())
	defer util.Lock.UnLock(types.BcsDaemonset{}, daemon.GetUuid())
	blog.Infof("check daemonset(%s) taskgroup status and force(%t) delete it", daemon.GetUuid(), daemon.ForceDeleting)
	//whether have running taskgroup
	hasRunning := false
	//check taskgroup whether exit, if taskgroup is running, then kill it
	for _, podId := range daemon.Pods {
		taskgroup, err := s.store.FetchTaskGroup(podId.Name)
		if err != nil {
			blog.Errorf("delete daemonset(%s) Fetch TaskGroup(%s) error %s", daemon.GetUuid(), podId.Name, err.Error())
			continue
		}

		//if running, kill it
		if !task.IsTaskGroupEnd(taskgroup) {
			hasRunning = true
			//
			if task.CanTaskGroupShutdown(taskgroup) {
				blog.Info("delete daemonset(%s): taskGroup(%s) not int end status, kill it", daemon.GetUuid(), taskgroup.ID)
				s.KillTaskGroup(taskgroup)
			} else {
				blog.Info("delete daemonset(%s): taskGroup(%s) not int end status at current", daemon.GetUuid(), taskgroup.ID)
			}
		}
	}

	//if have running taskgroup and not force deleting daemonset, waiting for taskgroup exit
	if hasRunning && !daemon.ForceDeleting {
		blog.Infof("daemonset(%s) have some running taskgroups, then waiting for taskgroup exit", daemon.GetUuid())
		return
	}

	//delete damonset taskgroup and daemonset definition
	//first delete taskgroup
	for _, podId := range daemon.Pods {
		taskgroup, err := s.store.FetchTaskGroup(podId.Name)
		if err != nil {
			blog.Errorf("delete daemonset(%s) Fetch TaskGroup(%s) error %s", daemon.GetUuid(), podId.Name, err.Error())
			continue
		}
		s.DeleteDaemonsetTaskGroup(daemon, taskgroup)
		blog.Infof("delete daemonset(%s) TaskGroup(%s) success", daemon.GetUuid(), podId.Name)
	}
	//delete versions
	versions, err := s.store.ListVersions(daemon.NameSpace, daemon.Name)
	if err != nil {
		blog.Errorf("delete daemonset(%s) Fetch versions failed %s", daemon.GetUuid(), err.Error())
	}
	for _, no := range versions {
		err = s.store.DeleteVersion(daemon.NameSpace, daemon.Name, no)
		if err != nil {
			blog.Errorf("delete daemonset(%s) version(%s) failed %s", daemon.GetUuid(), no, err.Error())
			continue
		}
		blog.Infof("delete daemonset(%s) version(%s) success", daemon.GetUuid(), no)
	}
	//delete daemonset
	err = s.store.DeleteDaemonset(daemon.NameSpace, daemon.Name)
	if err != nil {
		blog.Errorf("delete daemonset(%s) error %s", daemon.GetUuid(), err.Error())
		return
	}
	blog.Infof("delete daemonset(%s) success", daemon.GetUuid())
}

// Delete a taskgroup:
// the taskgroup will delete from DB, application and service
func (s *Scheduler) DeleteDaemonsetTaskGroup(daemon *types.BcsDaemonset, taskGroup *types.TaskGroup) []*commtypes.BcsPodIndex {
	//update daemonset podIds list
	delete := -1
	for index, currPod := range daemon.Pods {
		if currPod.Name == taskGroup.ID {
			delete = index
			break
		}
	}
	//delete podId in daemonset pods
	if delete != -1 {
		daemon.LastUpdateTime = time.Now().Unix()
		daemon.Pods = append(daemon.Pods[:delete], daemon.Pods[delete+1:]...)
	}
	s.deleteTaskGroup(taskGroup)
	return daemon.Pods
}