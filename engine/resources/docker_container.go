// Mgmt
// Copyright (C) 2013-2021+ James Shubin and the project contributors
// Written by James Shubin <james@shubin.ca> and the project contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// +build !nodocker

package resources

import (
	"context"
	"fmt"
	"io/ioutil"
	"reflect"
	"regexp"
	"time"

	"github.com/purpleidea/mgmt/engine"
	"github.com/purpleidea/mgmt/engine/traits"
	"github.com/purpleidea/mgmt/util"
	"github.com/purpleidea/mgmt/util/errwrap"

	dockeropts "github.com/docker/cli/opts"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/volume/mounts"
	"github.com/docker/go-connections/nat"
)

const (
	// ContainerRunning is the running container state.
	ContainerRunning = "running"
	// ContainerStopped is the stopped container state.
	ContainerStopped = "stopped"
	// ContainerRemoved is the removed container state.
	ContainerRemoved = "removed"

	// initCtxTimeout is the length of time, in seconds, before requests are
	// cancelled in Init.
	initCtxTimeout = 20
	// checkApplyCtxTimeout is the length of time, in seconds, before
	// requests are cancelled in CheckApply.
	checkApplyCtxTimeout = 120
)

func init() {
	engine.RegisterResource("docker:container", func() engine.Res { return &DockerContainerRes{} })
}

// DockerContainerRes is a docker container resource.
type DockerContainerRes struct {
	traits.Base // add the base methods without re-implementation
	traits.Edgeable

	// State of the container must be running, stopped, or removed.
	State string `lang:"state"`
	// Cmd is a command, or list of commands to run on the container.
	Cmd []string `lang:"cmd"`
	// DNS is a list of custom DNS servers
	DNS []string `lang:"dns"`
	// Devices is a list of device mappings
	Devices []string `lang:"devices"`
	// Domainname is the Domain name of the container
	Domainname string `lang:"domainname"`
	// Env is a list of environment variables. E.g. ["VAR=val",].
	Env map[string]string `lang:"env"`
	// Hostname is the hostname of the container
	Hostname string `lang:"hostname"`
	// Image is a docker image, or image:tag.
	Image string `lang:"image"`
	// Labels is a list of metadata labels
	Labels map[string]string `lang:"labels"`
	// Ports is a map of port bindings. E.g. {"tcp" => {80 => 8080},}.
	Ports map[string]map[int64]int64 `lang:"ports"`
	// Restart is the policy used to determine how to restart the container
	Restart string `lang:"restart"`
	// parsed restart policy and assigned during Validate()
	restartPolicy container.RestartPolicy
	// User is the username/uid that will run the cmd inside the container
	User string `lang:"user"`
	// Volumes is a list of volume/bind-mount mappings
	Volumes []mount.Mount `lang:"volumes"`

	// APIVersion allows you to override the host's default client API
	// version.
	APIVersion string `lang:"apiversion"`

	// Force, if true, this will destroy and redeploy the container if the
	// image is incorrect.
	Force bool `lang:"force"`

	client *client.Client // docker api client

	init *engine.Init
}

// Default returns some sensible defaults for this resource.
func (obj *DockerContainerRes) Default() engine.Res {
	return &DockerContainerRes{
		State: "running",
	}
}

// Validate if the params passed in are valid data.
func (obj *DockerContainerRes) Validate() error {
	// validate state
	if obj.State != ContainerRunning && obj.State != ContainerStopped && obj.State != ContainerRemoved {
		return fmt.Errorf("state must be running, stopped or removed")
	}

	// make sure an image is specified
	if obj.Image == "" {
		return fmt.Errorf("image must be specified")
	}

	// validate env
	for key := range obj.Env {
		if key == "" {
			return fmt.Errorf("environment variable name cannot be empty")
		}
	}

	// validate ports
	for k, v := range obj.Ports {
		if k != "tcp" && k != "udp" && k != "sctp" {
			return fmt.Errorf("ports primary key should be tcp, udp or sctp")
		}
		for p, q := range v {
			if (p < 1 || p > 65535) || (q < 1 || q > 65535) {
				return fmt.Errorf("ports must be between 1 and 65535")
			}
		}
	}

	// validate volumes
	parser := mounts.NewParser(mounts.OSLinux)
	for i := range obj.Volumes {
		vol := &obj.Volumes[i]

		// Due to mgmt lang not having nil pointers, all structs come pre-initialised
		// which makes the volume parser unhappy. Remove the unused empty structs
		switch vol.Type {
		case mount.TypeBind:
			vol.VolumeOptions = nil
			vol.TmpfsOptions = nil
		case mount.TypeVolume:
			vol.BindOptions = nil
			vol.TmpfsOptions = nil
		case mount.TypeTmpfs:
			vol.BindOptions = nil
			vol.VolumeOptions = nil
		}

		if err := parser.ValidateMountConfig(vol); err != nil {
			return err
		}
	}

	// validate APIVersion
	if obj.APIVersion != "" {
		verOK, err := regexp.MatchString(`^(v)[1-9]\.[0-9]\d*$`, obj.APIVersion)
		if err != nil {
			return errwrap.Wrapf(err, "error matching apiversion string")
		}
		if !verOK {
			return fmt.Errorf("invalid apiversion: %s", obj.APIVersion)
		}
	}

	// validate restart policy
	policy, err := dockeropts.ParseRestartPolicy(obj.Restart)
	if err != nil {
		return fmt.Errorf("invalid restart policy: %s", err)
	}
	if !(policy.IsAlways() || policy.IsNone() ||
		policy.IsOnFailure() || policy.IsUnlessStopped()) {
		return fmt.Errorf("policy must be always, on-failure, unless-stopped or no")
	}
	obj.restartPolicy = container.RestartPolicy(policy)

	return nil
}

// Init runs some startup code for this resource.
func (obj *DockerContainerRes) Init(init *engine.Init) error {
	var err error
	obj.init = init // save for later

	ctx, cancel := context.WithTimeout(context.Background(), initCtxTimeout*time.Second)
	defer cancel()

	// Initialize the docker client.
	obj.client, err = client.NewClient(client.DefaultDockerHost, obj.APIVersion, nil, nil)
	if err != nil {
		return errwrap.Wrapf(err, "error creating docker client")
	}

	// Validate the image.
	resp, err := obj.client.ImageSearch(ctx, obj.Image, types.ImageSearchOptions{Limit: 1})
	if err != nil {
		return errwrap.Wrapf(err, "error searching for image")
	}
	if len(resp) == 0 {
		return fmt.Errorf("image: %s not found", obj.Image)
	}
	return nil
}

// Close is run by the engine to clean up after the resource is done.
func (obj *DockerContainerRes) Close() error {
	return obj.client.Close() // close the docker client
}

// Watch is the primary listener for this resource and it outputs events.
func (obj *DockerContainerRes) Watch() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan, errChan := obj.client.Events(ctx, types.EventsOptions{})

	obj.init.Running() // when started, notify engine that we're running

	var send = false // send event?
	for {
		select {
		case event, ok := <-eventChan:
			if !ok { // channel shutdown
				return nil
			}
			if obj.init.Debug {
				obj.init.Logf("event received: Type=%s Action=%s Actor=%s",
					event.Type, event.Action, event.Actor.ID,
				)
			}
			send = true

		case err, ok := <-errChan:
			if !ok {
				return nil
			}
			return err

		case <-obj.init.Done: // closed by the engine to signal shutdown
			obj.init.Logf("done")
			return nil
		}

		// do all our event sending all together to avoid duplicate msgs
		if send {
			send = false
			obj.init.Event() // notify engine of an event (this can block)
		}
	}
}

// CheckApply method for Docker resource.
func (obj *DockerContainerRes) CheckApply(apply bool) (bool, error) {
	var id string
	var destroy bool

	ctx, cancel := context.WithTimeout(context.Background(), checkApplyCtxTimeout*time.Second)
	defer cancel()

	// List any container whose name matches this resource.
	opts := types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", obj.Name())),
	}
	containerList, err := obj.client.ContainerList(ctx, opts)
	if err != nil {
		return false, errwrap.Wrapf(err, "error listing containers")
	}

	// this should never happen
	if len(containerList) > 1 {
		return false, fmt.Errorf("more than one container named %s", obj.Name())
	}
	if len(containerList) == 0 && obj.State == ContainerRemoved {
		return true, nil
	}

	if len(containerList) == 1 {
		// inspect the container for all the gory volume/network details
		ctr, err := obj.client.ContainerInspect(ctx, containerList[0].ID)
		if err != nil {
			return false, errwrap.Wrapf(err, "error inspecting container: %s", containerList[0].ID)
		}

		id = ctr.ID // save the id for later

		// Check first all properties that require the container to be recreated
		// in case we pointlessly change some properties, but destroy the
		// container afterwards and recreate it correctly.
		for name, fn := range cmpFns {
			if !fn(ctr, obj) {
				obj.init.Logf("recreating container: property '%s' does not match", name)
				destroy = true
				break
			}
		}

		// environment check requires context for Docker API lookup
		envEqual, err := obj.compareEnv(ctx, ctr.Image, ctr.Config.Env)
		if err != nil || !envEqual {
			destroy = true
		}

		if !destroy && ctr.State.Status == ContainerRunning {
			// Final checks are to ensure all updateable configurables are
			// correct and don't need to be changed.
			return obj.containerUpdate(ctx, id, apply)
		}

		// Return an error if the running state does not match and it cannot
		// be updated without destroying the container.
		if destroy && !obj.Force {
			return false, fmt.Errorf("%s exists but the config does not match", obj.Name())
		}
	}

	if !apply { // do nothing and inform whether we would have done something
		return !destroy, nil
	}

	if obj.State == ContainerStopped { // container exists and should be stopped
		return false, obj.containerStop(ctx, id, nil)
	}

	if obj.State == ContainerRemoved { // container exists and should be removed
		if err := obj.containerStop(ctx, id, nil); err != nil {
			return false, err
		}
		return false, obj.containerRemove(ctx, id, types.ContainerRemoveOptions{})
	}

	if destroy {
		if err := obj.containerStop(ctx, id, nil); err != nil {
			return false, err
		}
		if err := obj.containerRemove(ctx, id, types.ContainerRemoveOptions{}); err != nil {
			return false, err
		}
		containerList = []types.Container{} // zero the list
	}

	if len(containerList) == 0 { // no container was found
		// Download the specified image if it doesn't exist locally.
		p, err := obj.client.ImagePull(ctx, obj.Image, types.ImagePullOptions{})
		if err != nil {
			return false, errwrap.Wrapf(err, "error pulling image")
		}
		// Wait for the image to download, EOF signals that it's done.
		if _, err := ioutil.ReadAll(p); err != nil {
			return false, errwrap.Wrapf(err, "error reading image pull result")
		}

		// set up port bindings
		containerConfig := &container.Config{
			Cmd:          obj.Cmd,
			Domainname:   obj.Domainname,
			Env:          util.StrMapKeyEqualValue(obj.Env),
			ExposedPorts: make(map[nat.Port]struct{}),
			Hostname:     obj.Hostname,
			Image:        obj.Image,
			User:         obj.User,
		}

		hostConfig := &container.HostConfig{
			Mounts:        obj.Volumes,
			PortBindings:  make(map[nat.Port][]nat.PortBinding),
			RestartPolicy: obj.restartPolicy,
		}

		for k, v := range obj.Ports {
			for p, q := range v {
				containerConfig.ExposedPorts[nat.Port(k)] = struct{}{}
				hostConfig.PortBindings[nat.Port(fmt.Sprintf("%d/%s", p, k))] = []nat.PortBinding{
					{
						HostIP:   "0.0.0.0",
						HostPort: fmt.Sprintf("%d", q),
					},
				}
			}
		}

		c, err := obj.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, obj.Name())
		if err != nil {
			return false, errwrap.Wrapf(err, "error creating container")
		}
		id = c.ID
	}

	return false, obj.containerStart(ctx, id, types.ContainerStartOptions{})
}

var cmpFns = map[string]func(types.ContainerJSON, *DockerContainerRes) bool{
	"image": func(ctr types.ContainerJSON, res *DockerContainerRes) bool {
		return ctr.Config.Image == res.Image
	},
	"hostname": func(ctr types.ContainerJSON, res *DockerContainerRes) bool {
		return res.Hostname == "" || ctr.Config.Hostname == res.Hostname
	},
	"domainname": func(ctr types.ContainerJSON, res *DockerContainerRes) bool {
		return ctr.Config.Domainname == res.Domainname
	},
	"user": func(ctr types.ContainerJSON, res *DockerContainerRes) bool {
		return ctr.Config.User == res.User
	},
	"cmd": func(ctr types.ContainerJSON, res *DockerContainerRes) bool {
		// res.Cmd being nil indicates there is no cmd explicitly set.
		return res.Cmd == nil || util.StrSliceEqual(ctr.Config.Cmd, res.Cmd)
	},
	"volumes": func(ctr types.ContainerJSON, res *DockerContainerRes) bool {
		if len(res.Volumes) != len(ctr.Mounts) {
			return false
		}

		for i, vol := range res.Volumes {
			var mnt *mount.Mount = nil
			// find the matching Mount to this Volume
			for j := range ctr.HostConfig.Mounts {
				if vol.Target == ctr.HostConfig.Mounts[j].Target {
					mnt = &ctr.HostConfig.Mounts[j]
					break
				}
			}
			// Not found, or found but doesn't match
			if mnt == nil || !reflect.DeepEqual(&res.Volumes[i], mnt) {
				return false
			}
		}
		return true
	},
}

func (obj *DockerContainerRes) containerUpdate(ctx context.Context, id string, apply bool) (bool, error) {
	changed := false

	ctr, err := obj.client.ContainerInspect(ctx, id)
	if err != nil {
		return false, errwrap.Wrapf(err, "error inspecting container: %s", id)
	}

	// check properties that can be changed at runtime
	if !ctr.HostConfig.RestartPolicy.IsSame(&obj.restartPolicy) {
		if !apply {
			return false, nil
		}
		obj.init.Logf("updating restart policy")
		_, err = obj.client.ContainerUpdate(ctx, id, container.UpdateConfig{
			RestartPolicy: obj.restartPolicy,
		})
		if err != nil {
			return false, errwrap.Wrapf(err, "failed to update restart policy")
		}
		changed = true
	}

	return !changed, nil
}

// compareEnv compares the environment of the running container with the obj.Env excluding the environment set in the container backing image returning true if the running container environment matches the obj.Env
func (obj *DockerContainerRes) compareEnv(ctx context.Context, image string, env []string) (bool, error) {
	// get the image env to remove the vars set in the image
	img, _, err := obj.client.ImageInspectWithRaw(ctx, image)
	if err != nil {
		return false, err
	}

	// []string{key=value} representation of the map[string]string
	objEnv := util.StrMapKeyEqualValue(obj.Env)

	var runningEnv []string
	for _, envVar := range env {
		// skip vars that are specified by the container and also not explicitly
		// specified in the container config
		if util.StrInList(envVar, img.ContainerConfig.Env) && !util.StrInList(envVar, objEnv) {
			continue
		}
		runningEnv = append(runningEnv, envVar)
	}

	return util.StrSliceEqual(runningEnv, objEnv), nil
}

/*
// compareLabels compares the Labels of the running container with the obj.Labels
// excluding the Labels set in the container backing image returning
// true if the running container Labels matches the obj.Labels
func (obj *DockerContainerRes) compareLabels(ctx context.Context, image string, Labels []string) (bool, error) {
	// get the image Labels to remove the vars set in the image
	img, _, err := obj.client.ImageInspectWithRaw(ctx, image)
	if err != nil {
		return false, err
	}

	// []string{key=value} representation of the map[string]string
	objLabels := util.StrMapKeyEqualValue(obj.Labels)

	var runningLabels []string
	for _, LabelsVar := range Labels {
		// skip vars that are specified by the container and also not explicitly
		// specified in the container config
		if util.StrInList(LabelsVar, img.ContainerConfig.Labels) && !util.
			StrInList(LabelsVar, objLabels) {
			continue
		}
		runningLabels = append(runningLabels, LabelsVar)
	}

	return util.StrSliceEqual(runningLabels, objLabels), nil
}
*/

// containerStart starts the specified container, and waits for it to start.
func (obj *DockerContainerRes) containerStart(ctx context.Context, id string, opts types.ContainerStartOptions) error {
	// Get an events channel for the container we're about to start.
	eventOpts := types.EventsOptions{
		Filters: filters.NewArgs(filters.Arg("container", id)),
	}
	eventCh, errCh := obj.client.Events(ctx, eventOpts)
	// Start the container.
	if err := obj.client.ContainerStart(ctx, id, opts); err != nil {
		return err
	}
	// Wait for a message on eventChan that says the container has started.
	select {
	case event := <-eventCh:
		if event.Status != "start" {
			return fmt.Errorf("unexpected event: %+v", event)
		}
	case err := <-errCh:
		return errwrap.Wrapf(err, "error waiting for container start")
	}
	return nil
}

// containerStop stops the specified container and waits for it to stop.
func (obj *DockerContainerRes) containerStop(ctx context.Context, id string, timeout *time.Duration) error {
	ch, errCh := obj.client.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	obj.client.ContainerStop(ctx, id, timeout)
	select {
	case <-ch:
	case err := <-errCh:
		return errwrap.Wrapf(err, "error waiting for container to stop")
	}
	return nil
}

// containerRemove removes the specified container and waits for it to be
// removed.
func (obj *DockerContainerRes) containerRemove(ctx context.Context, id string, opts types.ContainerRemoveOptions) error {
	ch, errCh := obj.client.ContainerWait(ctx, id, container.WaitConditionRemoved)
	obj.client.ContainerRemove(ctx, id, opts)
	select {
	case <-ch:
	case err := <-errCh:
		return errwrap.Wrapf(err, "error waiting for container to be removed")
	}
	return nil
}

// Cmp compares two resources and returns an error if they are not equivalent.
func (obj *DockerContainerRes) Cmp(r engine.Res) error {
	// we can only compare DockerContainerRes to others of the same resource kind
	res, ok := r.(*DockerContainerRes)
	if !ok {
		return fmt.Errorf("error casting r to *DockerContainerRes")
	}

	if obj.State != res.State {
		return fmt.Errorf("the State differs")
	}
	if obj.Image != res.Image {
		return fmt.Errorf("the Image differs")
	}
	if err := util.SortedStrSliceCompare(obj.Cmd, res.Cmd); err != nil {
		return errwrap.Wrapf(err, "the Cmd field differs")
	}
	// if err := util.SortedStrSliceCompare(obj.Env, res.Env); err != nil {
	// 	return errwrap.Wrapf(err, "tne Env field differs")
	// }
	if len(obj.Ports) != len(res.Ports) {
		return fmt.Errorf("the Ports length differs")
	}
	for k, v := range obj.Ports {
		for p, q := range v {
			if w, ok := res.Ports[k][p]; !ok || q != w {
				return fmt.Errorf("the Ports field differs")
			}
		}
	}
	if obj.APIVersion != res.APIVersion {
		return fmt.Errorf("the APIVersion differs")
	}
	if obj.Force != res.Force {
		return fmt.Errorf("the Force field differs")
	}
	return nil
}

// DockerContainerUID is the UID struct for DockerContainerRes.
type DockerContainerUID struct {
	engine.BaseUID

	name string
}

// DockerContainerResAutoEdges holds the state of the auto edge generator.
type DockerContainerResAutoEdges struct {
	UIDs    []engine.ResUID
	pointer int
}

// AutoEdges returns edges to any docker:image resource that matches the image
// specified in the docker:container resource definition.
func (obj *DockerContainerRes) AutoEdges() (engine.AutoEdge, error) {
	var result []engine.ResUID
	var reversed bool
	if obj.State != "removed" {
		reversed = true
	}
	result = append(result, &DockerImageUID{
		BaseUID: engine.BaseUID{
			Reversed: &reversed,
		},
		image: dockerImageNameTag(obj.Image),
	})
	return &DockerContainerResAutoEdges{
		UIDs:    result,
		pointer: 0,
	}, nil
}

// Next returns the next automatic edge.
func (obj *DockerContainerResAutoEdges) Next() []engine.ResUID {
	if len(obj.UIDs) == 0 {
		return nil
	}
	value := obj.UIDs[obj.pointer]
	obj.pointer++
	return []engine.ResUID{value}
}

// Test gets results of the earlier Next() call, & returns if we should
// continue.
func (obj *DockerContainerResAutoEdges) Test(input []bool) bool {
	if len(obj.UIDs) <= obj.pointer {
		return false
	}
	if len(input) != 1 { // in case we get given bad data
		panic(fmt.Sprintf("Expecting a single value!"))
	}
	return true // keep going
}

// UIDs includes all params to make a unique identification of this object. Most
// resources only return one, although some resources can return multiple.
func (obj *DockerContainerRes) UIDs() []engine.ResUID {
	x := &DockerContainerUID{
		BaseUID: engine.BaseUID{Name: obj.Name(), Kind: obj.Kind()},
		name:    obj.Name(),
	}
	return []engine.ResUID{x}
}

// UnmarshalYAML is the custom unmarshal handler for this struct. It is
// primarily useful for setting the defaults.
func (obj *DockerContainerRes) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawRes DockerContainerRes // indirection to avoid infinite recursion

	def := obj.Default()                 // get the default
	res, ok := def.(*DockerContainerRes) // put in the right format
	if !ok {
		return fmt.Errorf("could not convert to DockerContainerRes")
	}
	raw := rawRes(*res) // convert; the defaults go here

	if err := unmarshal(&raw); err != nil {
		return err
	}

	*obj = DockerContainerRes(raw) // restore from indirection with type conversion!
	return nil
}
