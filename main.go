/**
 * Copyright (c) 2016 Intel Corporation
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
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gocraft/web"

	cephClient "github.com/trustedanalytics/tap-ceph-broker/client"
	cephModel "github.com/trustedanalytics/tap-ceph-broker/model"
	httpGoCommon "github.com/trustedanalytics/tap-go-common/http"
	"github.com/trustedanalytics/tap-go-common/util"
)

type Context struct{}

func (c *Context) GetHealth(rw web.ResponseWriter, req *web.Request) {
	rw.WriteHeader(http.StatusOK)
}

func main() {
	log.Println("Starting...")
	go monitor()

	context := Context{}
	r := web.New(context)
	r.Middleware(web.LoggerMiddleware)
	r.Get("/healthz", (*Context).GetHealth)

	log.Println("Starting HTTP Server...")
	httpGoCommon.StartServer(r)
}

func getCephBrokerConnector() (*cephClient.CephBrokerConnector, error) {
	address, username, password, err := util.GetConnectionParametersFromEnv("CEPH_BROKER")
	if err != nil {
		return nil, err
	}
	return cephClient.NewCephBrokerBasicAuth("http://"+address, username, password)
}

type NodeStatus struct {
	Name  string
	Ip    string
	Ready string
}

func GetNodeStatuses() ([]NodeStatus, error) {
	log.Println("GetNodeStatuses")
	ret := []NodeStatus{}

	out, err := exec.Command("kubectl", "get", "nodes", "-o=json").Output() // "-o=custom-columns=NAME:.metadata.name,IP:.status.addresses[0].address,STATUS:.status.conditions[2].status").Output()
	if err != nil {
		log.Println(err)
		return ret, err
	}

	jsonparser.ArrayEach(out, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		name, err := jsonparser.GetString(value, "metadata", "name")
		if err != nil {
			log.Println(err)
		}
		ip := ""
		jaddresses, _, _, err := jsonparser.Get(value, "status", "addresses")
		if err != nil {
			log.Println(err)
		} else {
			jsonparser.ArrayEach(jaddresses, func(address_value []byte, dataType jsonparser.ValueType, offset int, err error) {
				jtype, err := jsonparser.GetString(address_value, "type")
				if err != nil {
					log.Println(err)
					return
				}

				if jtype == "InternalIP" {
					ip, err = jsonparser.GetString(address_value, "address")
					if err != nil {
						log.Println(err)
					}
				}
			})
		}

		status := ""
		jconditions, _, _, err := jsonparser.Get(value, "status", "conditions")
		if err != nil {
			log.Println(err)
		} else {
			jsonparser.ArrayEach(jconditions, func(condition_value []byte, dataType jsonparser.ValueType, offset int, err error) {
				jtype, err := jsonparser.GetString(condition_value, "type")
				if err != nil {
					log.Println(err)
					return
				}
				if jtype == "Ready" {
					status, err = jsonparser.GetString(condition_value, "status")
					if err != nil {
						panic(err)
					}
				}
			})
		}

		log.Println(string(name), string(ip), string(status))
		node_status := NodeStatus{name, ip, status}
		ret = append(ret, node_status)
	}, "items")

	return ret, nil
}

func GetUnhealthyNodes() ([]NodeStatus, error) {
	log.Println("GetUnhealthyNodes")
	ret := []NodeStatus{}
	node_statuses, err := GetNodeStatuses()
	if err != nil {
		return ret, err
	}
	for _, n := range node_statuses {
		log.Print("Checking if node", n, "is healthy...")
		if n.Ready != "True" {
			ret = append(ret, n)
			log.Println("It is NOT!")
		} else {
			log.Println("...Healthy")
		}
	}
	return ret, nil
}

func GetLocks() (map[string][]cephModel.Lock, error) {
	log.Println("GetLocks")
	node_lock_map := map[string][]cephModel.Lock{}
	ceph_broker_client, err := getCephBrokerConnector()
	if err != nil {
		log.Println(err)
		return node_lock_map, err
	}
	locks, err := ceph_broker_client.ListLocks()
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
	log.Println("DeleteLock", lock)
	ceph_broker_client, err := getCephBrokerConnector()
	if err != nil {
		log.Println(err)
		return err
	}
	status, err := ceph_broker_client.DeleteLock(lock)
	if err != nil {
		return err
	}
	log.Println("Removal status:", status)
	return nil
}

/*
 *  Implements STONITH functionality for Ceph locks in case of catastrophic node failure.
 *  There is still a hope (pull request #??????) that it won't needed in k8s 1.6+.
 */
func monitor() {
	log.Println("monitor started!")

	for {
		time.Sleep(10 * time.Second)

		node_lock_map, err := GetLocks()
		log.Println(node_lock_map)

		node_statuses, err := GetUnhealthyNodes()
		if err != nil {
			log.Println(err)
			continue
		}

		if len(node_statuses) > 0 {
			for _, unhealthy_node := range node_statuses {
				log.Println("Evicting unhealthy node:", unhealthy_node)
				out, err := exec.Command("kubectl", "drain", unhealthy_node.Name, "--force").Output()
				if err != nil {
					log.Println(err)
				}
				log.Printf("Output: %s\n", string(out))

				log.Println("Removing all locks from unhealthy node:", unhealthy_node)
				for _, l := range node_lock_map[unhealthy_node.Name] {
					log.Println(" removing lock ---> ", l)
					err = DeleteLock(l)
					if err != nil {
						panic(err)
					}
				}
			}
		} else {
			log.Println("No unhealthy nodes.")
		}
	}
}
