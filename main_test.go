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
	"os"
	"testing"
)

type byteArray []byte

const (
	nodeItem1 string = `{
		"metadata":{
				"name":"test1"
			},
		"status":{
			"addresses":[{
				"address":"1",
				"type":"InternalIP"
			}],
			"conditions":[{
				"type":"Ready",
				"status":"OK"
			}]
		}
	}`
)

func TestNodeStatusExtractIPShouldReturnErrorForBadJson(t *testing.T) {
	_, e := nodeStatusExtractIP([]byte(`{
		"status":{
			"no_addresses":"1"
		}
	}`))
	if e == nil {
		t.Error("Error should not be nil")
	}
}
func TestNodeStatusExtractIPShouldReturnIpAddress(t *testing.T) {
	ip, e := nodeStatusExtractIP([]byte(nodeItem1))

	if e != nil {
		t.Error("Error should be nil")
	}
	if ip != "1" {
		t.Error("Ip Address should be 1")
	}
}

func TestNodeStatusExtractStatusShouldReturnStatus(t *testing.T) {
	status, e := nodeStatusExtractStatus([]byte(nodeItem1))

	if e != nil {
		t.Error("Error should be nil")
	}
	if status != "OK" {
		t.Error("Status should be OK")
	}
}

func TestNodeStatusExtractStatusShouldReturnError(t *testing.T) {
	_, e := nodeStatusExtractStatus([]byte(""))

	if e == nil {
		t.Error("Error should not be nil")
	}
}

func TestExtractNodeStatuses(t *testing.T) {
	statuses, err := extractNodeStatuses([]byte(`{
		"items":[` + nodeItem1 + `]
	}`))

	if err != nil {
		t.Error("Error should be nil")
	}

	if len(statuses) != 1 {
		t.Error("One status should be returned")
	} else {
		if statuses[0].Name != "test1" {
			t.Error("Status test1 should be returned")
		}
		if statuses[0].Ip != "1" {
			t.Error("Ip node should be 1")
		}
		if statuses[0].Ready != "OK" {
			t.Error("Status node should be OK")
		}
	}

}

func TestGetCephBrokerConnectorWithoutEnvs(t *testing.T) {
	_, err := getCephBrokerConnector()
	if err == nil {
		t.Error("Error should not be nil")
	}
}

func TestGetCephBrokerConnectorWithEnvs(t *testing.T) {
	component := "CEPH_BROKER"
	os.Setenv(component+"_HOST", "host")
	os.Setenv(component+"_PORT", "80")
	os.Setenv(component+"_USER", "user")
	os.Setenv(component+"_PASS", "password")
	_, err := getCephBrokerConnector()
	if err != nil {
		t.Errorf("Error should be nil, but is: %v", err)
	}
}
