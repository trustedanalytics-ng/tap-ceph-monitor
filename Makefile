# Copyright (c) 2016 Intel Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
GOBIN=$(GOPATH)/bin
APP_DIR_LIST=$(shell go list ./... | grep -v /vendor/)

build: verify_gopath
	CGO_ENABLED=0 go install -tags netgo $(APP_DIR_LIST)
	go fmt $(APP_DIR_LIST)

verify_gopath:
	@if [ -z "$(GOPATH)" ] || [ "$(GOPATH)" = "" ]; then\
		echo "GOPATH not set. You need to set GOPATH before run this command";\
		exit 1 ;\
	fi

docker_build: build_anywhere
	test -s kubectl
	chmod +x kubectl
	docker build -t tap-ceph-monitor .

push_docker: docker_build
	docker tag -f tap-ceph-monitor $(REPOSITORY_URL)/tap-ceph-monitor:latest
	docker push $(REPOSITORY_URL)/tap-ceph-monitor:latest

kubernetes_deploy: docker_build
	kubectl create -f configmap.yaml
	kubectl create -f service.yaml
	kubectl create -f deployment.yaml

kubernetes_update: docker_build
	kubectl delete -f deployment.yaml
	kubectl create -f deployment.yaml

deps_fetch_specific: bin/govendor
	@if [ "$(DEP_URL)" = "" ]; then\
		echo "DEP_URL not set. Run this comand as follow:";\
		echo " make deps_fetch_specific DEP_URL=github.com/nu7hatch/gouuid";\
	exit 1 ;\
	fi
	@echo "Fetching specific dependency in newest versions"
	$(GOBIN)/govendor fetch -v $(DEP_URL)

deps_update_tap: verify_gopath
	$(GOBIN)/govendor update github.com/trustedanalytics/...
	rm -Rf vendor/github.com/trustedanalytics/tap-ceph-monitor
	@echo "Done"

prepare_dirs:
	mkdir -p ./temp/src/github.com/trustedanalytics/tap-ceph-monitor
	$(eval REPOFILES=$(shell pwd)/*)
	ln -sf $(REPOFILES) temp/src/github.com/trustedanalytics/tap-ceph-monitor

build_anywhere: prepare_dirs
	$(eval GOPATH=$(shell cd ./temp; pwd))
	$(eval APP_DIR_LIST=$(shell GOPATH=$(GOPATH) go list ./temp/src/github.com/trustedanalytics/tap-ceph-monitor/... | grep -v /vendor/))
	GOPATH=$(GOPATH) CGO_ENABLED=0 go build -tags netgo $(APP_DIR_LIST)
	rm -Rf application && mkdir application
	cp ./tap-ceph-monitor ./application/tap-ceph-monitor
	rm -Rf ./temp

install_mockgen:
	scripts/install_mockgen.sh

mock_update:
	$(GOBIN)/mockgen -source=k8s/k8sfabricator.go -package=k8s -destination=k8s/k8sfabricator_mock.go
	$(GOBIN)/mockgen -source=vendor/github.com/trustedanalytics/tap-catalog/client/client.go -package=api -destination=api/catalog_mock_test.go
	$(GOBIN)/mockgen -source=vendor/github.com/trustedanalytics/tap-template-repository/client/client_api.go -package=api -destination=api/template_mock_test.go
	$(GOBIN)/mockgen -source=vendor/github.com/trustedanalytics/tap-ca/client/client.go -package=api -destination=api/ca_mock_test.go
	./add_license.sh

test: verify_gopath
	go test -tags netgo --cover $(APP_DIR_LIST)


