module github.com/purpleidea/mgmt

go 1.16

require (
	github.com/Microsoft/hcsshim v0.8.16 // indirect
	github.com/aws/aws-sdk-go v1.38.30
	github.com/containerd/continuity v0.1.0 // indirect
	github.com/coredhcp/coredhcp v0.0.0-20210426135022-eface94a0dd7
	github.com/coreos/go-systemd/v22 v22.3.1
	github.com/cyphar/filepath-securejoin v0.2.2
	github.com/davecgh/go-spew v1.1.1
	github.com/deniswernert/go-fstab v0.0.0-20141204152952-eb4090f26517
	github.com/docker/docker v20.10.6+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/godbus/dbus/v5 v5.0.4
	github.com/hashicorp/consul/api v1.8.1
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/hil v0.0.0-20201113172851-43f73a9c7007
	github.com/iancoleman/strcase v0.1.3
	github.com/insomniacslk/dhcp v0.0.0-20210428091707-95b2ff6905c9
	github.com/kevinburke/go-bindata v3.22.0+incompatible // indirect
	github.com/kylelemons/godebug v1.1.0
	github.com/libvirt/libvirt-go v7.0.0+incompatible
	github.com/libvirt/libvirt-go-xml v7.2.0+incompatible
	github.com/moby/sys/mount v0.2.0 // indirect
	github.com/moby/term v0.0.0-20201216013528-df9cb8a40635 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/pborman/uuid v1.2.1
	github.com/pin/tftp v0.0.0-20200229063000-e4f073737eb2
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.10.0
	github.com/sanity-io/litter v1.5.0
	github.com/spf13/afero v1.6.0
	github.com/urfave/cli/v2 v2.3.0
	github.com/vishvananda/netlink v1.1.0
	go.etcd.io/etcd/api/v3 v3.5.0-alpha.0
	go.etcd.io/etcd/client/v3 v3.5.0-alpha.0
	go.etcd.io/etcd/pkg/v3 v3.5.0-alpha.0
	go.etcd.io/etcd/server/v3 v3.5.0-alpha.0
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b
	golang.org/x/sys v0.0.0-20210426230700-d19ff857e887
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba
	gopkg.in/fsnotify.v1 v1.4.7
	gopkg.in/src-d/go-git.v4 v4.13.1
	gopkg.in/yaml.v2 v2.4.0
	honnef.co/go/augeas v0.0.0-20161110001225-ca62e35ed6b8
)

replace github.com/docker/docker/internal/testutil => gotest.tools/v3 v3.0.3
