# tap-ceph-monitor
This application monitors health of Kubernetes nodes. If any of them is unhealthy, it will unlock Ceph volumes in order to make them schedulable on other workers. This application will be obsolete around Kubernetes 1.6, where Ceph volume manager is rewritten (volumes handled by master, not by each kubelet).
