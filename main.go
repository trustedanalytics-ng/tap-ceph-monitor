/**
 * Copyright (c) 2017 Intel Corporation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gocraft/web"

	"github.com/trustedanalytics-ng/tap-ceph-broker/client"
	cephModel "github.com/trustedanalytics-ng/tap-ceph-broker/model"
	httpGoCommon "github.com/trustedanalytics-ng/tap-go-common/http"
	commonLogger "github.com/trustedanalytics-ng/tap-go-common/logger"
	"github.com/trustedanalytics-ng/tap-go-common/util"
)

var logger, _ = commonLogger.InitLogger("ceph-monitor")
var cephClient client.CephBroker

type Context struct{}

func (c *Context) GetHealth(rw web.ResponseWriter, req *web.Request) {
	rw.WriteHeader(http.StatusOK)
}

func main() {
	var err error
	cephClient, err = getCephBrokerConnector()
	if err != nil {
		logger.Fatal("Can't connect with TAP-ceph-broker!", err)
	}

	logger.Info("Starting...")
	go monitor()

	context := Context{}
	r := web.New(context)
	r.Middleware(web.LoggerMiddleware)
	r.Get("/healthz", (*Context).GetHealth)

	logger.Info("Starting HTTP Server...")
	httpGoCommon.StartServer(r)
}

func getCephBrokerConnector() (*client.CephBrokerConnector, error) {
	address, username, password, err := util.GetConnectionParametersFromEnv("CEPH_BROKER")
	if err != nil {
		return nil, err
	}
	return client.NewCephBrokerBasicAuth("https://"+address, username, password)
}

type NodeStatus struct {
	Name  string
	Ip    string
	Ready string
}

func nodeStatusExtractIP(value []byte) (string, error) {
	ip := ""
	jaddresses, _, _, err := jsonparser.Get(value, "status", "addresses")
	if err != nil {
		logger.Error(err)
		return ip, err
	}

	jsonparser.ArrayEach(jaddresses, func(address_value []byte, dataType jsonparser.ValueType, offset int, err error) {
		jtype, err := jsonparser.GetString(address_value, "type")
		if err != nil {
			logger.Error(err)
			return
		}

		if jtype == "InternalIP" {
			ip, err = jsonparser.GetString(address_value, "address")
			if err != nil {
				logger.Error(err)
			}
		}
	})

	return ip, err
}

func nodeStatusExtractStatus(value []byte) (string, error) {
	status := ""
	jconditions, _, _, err := jsonparser.Get(value, "status", "conditions")
	if err != nil {
		logger.Error(err)
		return status, err
	}
	jsonparser.ArrayEach(jconditions, func(condition_value []byte, dataType jsonparser.ValueType, offset int, err error) {
		jtype, err := jsonparser.GetString(condition_value, "type")
		if err != nil {
			logger.Error(err)
			return
		}
		if jtype == "Ready" {
			status, err = jsonparser.GetString(condition_value, "status")
			if err != nil {
				logger.Error(err)
			}
		}
	})

	return status, nil
}

func extractNodeStatuses(valueNodes []byte) ([]NodeStatus, error) {
	result := []NodeStatus{}
	_, errParse := jsonparser.ArrayEach(valueNodes, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		name, err := jsonparser.GetString(value, "metadata", "name")
		if err != nil {
			logger.Error(err)
		}
		ip, err := nodeStatusExtractIP(value)
		status, err := nodeStatusExtractStatus(value)
		logger.Info(string(name), string(ip), string(status))
		node_status := NodeStatus{Name: name, Ip: ip, Ready: status}
		result = append(result, node_status)
	}, "items")
	return result, errParse
}

func GetNodeStatuses() ([]NodeStatus, error) {
	logger.Info("GetNodeStatuses")

	out, err := exec.Command("kubectl", "get", "nodes", "-o=json").Output()
	if err != nil {
		logger.Error(err)
		return []NodeStatus{}, err
	}

	return extractNodeStatuses(out)
}

func GetUnhealthyNodes() ([]NodeStatus, error) {
	logger.Info("GetUnhealthyNodes")
	ret := []NodeStatus{}
	node_statuses, err := GetNodeStatuses()
	if err != nil {
		return ret, err
	}
	for _, n := range node_statuses {
		logger.Info("Checking if node", n, "is healthy...")
		if n.Ready != "True" {
			ret = append(ret, n)
			logger.Info("It is NOT!")
		} else {
			logger.Info("...Healthy")
		}
	}
	return ret, nil
}

func GetLocks() (map[string][]cephModel.Lock, error) {
	logger.Info("GetLocks")
	node_lock_map := map[string][]cephModel.Lock{}

	locks, _, err := cephClient.ListLocks()
	if err != nil {
		return node_lock_map, err
	}

	for _, lock := range locks {
		// ImageName LockName Locker Address
		node_with_lock := strings.Replace(lock.LockName, "kubelet_lock_magic_", "", -1)

		_, ok := node_lock_map[node_with_lock]
		if !ok {
			node_lock_map[node_with_lock] = []cephModel.Lock{lock}
		} else {
			node_lock_map[node_with_lock] = append(node_lock_map[node_with_lock], lock)
		}
	}
	return node_lock_map, nil
}

func DeleteLock(lock cephModel.Lock) error {
	logger.Info("DeleteLock", lock)

	status, err := cephClient.DeleteLock(lock)
	if err != nil {
		return err
	}
	logger.Info("Removal status:", status)
	return nil
}

func evictNode(unhealthy_node NodeStatus) error {
	logger.Info("Evicting unhealthy node:", unhealthy_node)
	_, err := exec.Command("kubectl", "drain", unhealthy_node.Name, "--force").Output()
	if err != nil {
		logger.Error(err)
		return err
	}
	return nil
}

func removeAllLocks(unhealthy_node NodeStatus, node_lock_map map[string][]cephModel.Lock) error {
	logger.Info("Removing all locks from unhealthy node:", unhealthy_node)
	for _, l := range node_lock_map[unhealthy_node.Name] {
		logger.Info(" removing lock ---> ", l)
		if err := DeleteLock(l); err != nil {
			return err
		}
	}
	return nil
}

/*
 *  Implements STONITH functionality for Ceph locks in case of catastrophic node failure.
 *  There is still a hope (pull request https://github.com/kubernetes/kubernetes/pull/33660) that it won't needed in k8s 1.6+.
 */
func monitor() {
	logger.Info("monitor started!")

	for {
		time.Sleep(10 * time.Second)

		node_lock_map, err := GetLocks()
		logger.Info(node_lock_map)

		node_statuses, err := GetUnhealthyNodes()
		if err != nil {
			logger.Error(err)
			continue
		}

		if len(node_statuses) > 0 {
			for _, unhealthy_node := range node_statuses {
				evictNode(unhealthy_node)
				removeAllLocks(unhealthy_node, node_lock_map)
			}
		} else {
			logger.Info("No unhealthy nodes.")
		}
	}
}
