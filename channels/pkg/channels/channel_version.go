/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package channels

import (
	"encoding/json"
	"fmt"
	"github.com/blang/semver"
	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/v1"
	metav1 "k8s.io/kubernetes/pkg/apis/meta/v1"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/clientset"
	"strings"
)

const AnnotationPrefix = "addons.k8s.io/"

type Channel struct {
	Namespace string
	Name      string
}

type ChannelVersion struct {
	Version *string `json:"version,omitempty"`
	Channel *string `json:"channel,omitempty"`
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (c *ChannelVersion) String() string {
	return "Version=" + stringValue(c.Version) + " Channel=" + stringValue(c.Channel)
}

func ParseChannelVersion(s string) (*ChannelVersion, error) {
	v := &ChannelVersion{}
	err := json.Unmarshal([]byte(s), v)
	if err != nil {
		return nil, fmt.Errorf("error parsing version spec %q", s)
	}
	return v, nil
}

func FindAddons(ns *v1.Namespace) map[string]*ChannelVersion {
	addons := make(map[string]*ChannelVersion)
	for k, v := range ns.Annotations {
		if !strings.HasPrefix(k, AnnotationPrefix) {
			continue
		}

		channelVersion, err := ParseChannelVersion(v)
		if err != nil {
			glog.Warningf("failed to parse annotation %q=%q", k, v)
			continue
		}

		name := strings.TrimPrefix(k, AnnotationPrefix)
		addons[name] = channelVersion
	}
	return addons
}

func (c *ChannelVersion) Encode() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("error encoding version spec: %v", err)
	}
	return string(data), nil
}

func (c *Channel) AnnotationName() string {
	return AnnotationPrefix + c.Name
}

func (c *ChannelVersion) Replaces(existing *ChannelVersion) bool {
	if existing.Version != nil {
		if c.Version == nil {
			return false
		}
		cVersion, err := semver.Parse(*c.Version)
		if err != nil {
			glog.Warningf("error parsing version %q; will ignore this version", *c.Version)
			return false
		}
		existingVersion, err := semver.Parse(*existing.Version)
		if err != nil {
			glog.Warningf("error parsing existing version %q", *existing.Version)
			return true
		}
		return cVersion.GT(existingVersion)
	}

	glog.Warningf("ChannelVersion did not have a version; can't perform real version check")
	if c.Version == nil {
		return false
	}
	return true
}

func (c *Channel) GetInstalledVersion(k8sClient *clientset.Clientset) (*ChannelVersion, error) {
	ns, err := k8sClient.Namespaces().Get(c.Namespace, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error querying namespace %q: %v", c.Namespace, err)
	}

	annotationValue, ok := ns.Annotations[c.AnnotationName()]
	if !ok {
		return nil, nil
	}

	return ParseChannelVersion(annotationValue)
}

type annotationPatch struct {
	Metadata annotationPatchMetadata `json:"metadata,omitempty"`
}
type annotationPatchMetadata struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

func (c *Channel) SetInstalledVersion(k8sClient *clientset.Clientset, version *ChannelVersion) error {
	// Primarily to check it exists
	_, err := k8sClient.Namespaces().Get(c.Namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error querying namespace %q: %v", c.Namespace, err)
	}

	value, err := version.Encode()
	if err != nil {
		return err
	}

	annotationPatch := &annotationPatch{Metadata: annotationPatchMetadata{Annotations: map[string]string{c.AnnotationName(): value}}}
	annotationPatchJson, err := json.Marshal(annotationPatch)
	if err != nil {
		return fmt.Errorf("error building annotation patch: %v", err)
	}

	glog.V(2).Infof("sending patch: %q", string(annotationPatchJson))

	_, err = k8sClient.Namespaces().Patch(c.Namespace, api.StrategicMergePatchType, annotationPatchJson)
	if err != nil {
		return fmt.Errorf("error applying annotation to namespace: %v", err)
	}
	return nil
}
