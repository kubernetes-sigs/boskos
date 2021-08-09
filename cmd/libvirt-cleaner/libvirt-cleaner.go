/*
Copyright 2020 The Kubernetes Authors.
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

package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"os/exec"
	"strings"
)

func virsh (connect string, command string, grep_arg string) ([]string, error) {
	var b bytes.Buffer
	var err error

	logrus.Debugf("virsh -c %s %s --all --name | grep %s", connect, command, grep_arg)

	c1 := exec.Command("mock-nss.sh", "virsh", "-c", connect, command, "--all", "--name")
	c2 := exec.Command("grep", grep_arg)
	r, w := io.Pipe() 
	c1.Stdout = w
	c2.Stdin = r
	c2.Stdout = &b
	err = c1.Start()
	if err != nil {
		logrus.Infof("virsh c1.Start(): %s", err)
		return []string{""}, err
	}
	err = c2.Start()
	if err != nil {
		logrus.Infof("virsh c2.Start(): %s", err)
		return []string{""}, err
	}
	err = c1.Wait()
	if err != nil {
		logrus.Infof("virsh c1.Wait(): %s", err)
		return []string{""}, err
	}
	err = w.Close()
	if err != nil {
		logrus.Infof("virsh w.Close(): %s", err)
		return []string{""}, err
	}
	err = c2.Wait()
	if err != nil {
		logrus.Infof("virsh c2.Wait(): %s", err)
		return []string{""}, err
	}
	logrus.Infof("virsh %s output:\n%s", command, &b)

	return strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n"), nil
}

func get_domain (resource string) (string, error) {

	if strings.HasPrefix(resource, "libvirt-ppc64le-0-") {			// C155F2U33
		return "sshd-0.bastion-ppc64le-libvirt.svc.cluster.local", nil
	} else if strings.HasPrefix(resource, "libvirt-ppc64le-1-") {		// C155F2U31
		// return "10.10.1.10", nil					// Local testing
		return "sshd-1.bastion-ppc64le-libvirt.svc.cluster.local", nil
	} else if strings.HasPrefix(resource, "libvirt-ppc64le-2-") {		// C155F2U35
		return "sshd-2.bastion-ppc64le-libvirt.svc.cluster.local", nil
	} else if strings.HasPrefix(resource, "libvirt-s390x-0-") {		// lnxocp01
		return "sshd-0.bastion-z.svc.cluster.local", nil
	} else if strings.HasPrefix(resource, "libvirt-s390x-1-") {		// lnxocp02
		return "sshd-1.bastion-z.svc.cluster.local", nil
	}

	return "", errors.New(fmt.Sprintf("Unknown domain %s", resource))
}

func Cleanup (resource string) (bool, error) {

	logrus.Debugf("libvirt_cleaner.Cleanup (%s)", resource)

	domain, err := get_domain (resource)
	if err != nil {
		return false, err
	}

	connect := fmt.Sprintf("qemu+tcp://%s/system", domain)

	result, err := virsh (connect, "list", resource)
	if err != nil {
		return false, errors.New(fmt.Sprintf("virsh list %s: %v", resource, err))
	}
	logrus.Infof("executing virsh list %s: output: %q", resource, result)

	for _, domain := range result {
		logrus.Infof("executing virsh -c %s destroy %s", connect, domain)
		c1 := exec.Command("mock-nss.sh", "virsh", "-c", connect, "destroy", domain)
		o1, err1 := c1.CombinedOutput()
		if err != nil {
			return false, errors.New(fmt.Sprintf("virsh destroy %s %v", domain, err1))
		}
		logrus.Infof("c1 output: %s", o1)

		logrus.Infof("executing virsh -c %s undefine %s", connect, domain)
		c2 := exec.Command("mock-nss.sh", "virsh", "-c", connect, "undefine", domain)
		o2, err2 := c2.CombinedOutput()
		if err != nil {
			return false, errors.New(fmt.Sprintf("virsh undefine %s %v", domain, err2))
		}
		logrus.Infof("c2 output: %s", o2)
	}

	result, err = virsh (connect, "pool-list", resource)
	if err != nil {
		return false, errors.New(fmt.Sprintf("virsh pool-list %s: %v", resource, err))
	}
	logrus.Infof("executing virsh pool-list %s: output: %q", resource, result)

	for _, domain := range result {
		logrus.Infof("executing virsh -c %s pool-list %s", connect, domain)
		c1 := exec.Command("mock-nss.sh", "virsh", "-c", connect, "pool-destroy", domain)
		o1, err1 := c1.CombinedOutput()
		if err1 != nil {
			return false, errors.New(fmt.Sprintf("virsh pool-destroy %s %v", domain, err1))
		}
		logrus.Infof("c1 output: %s", o1)

		logrus.Infof("executing virsh -c %s pool-undefine %s", connect, domain)
		c2 := exec.Command("mock-nss.sh", "virsh", "-c", connect, "pool-undefine", domain)
		o2, err2 := c2.CombinedOutput()
		if err2 != nil {
			return false, errors.New(fmt.Sprintf("virsh pool-undefine %s %v", domain, err2))
		}
		logrus.Infof("c2 output: %s", o2)
	}

	result, err = virsh (connect, "net-list", resource)
	if err != nil {
		return false, errors.New(fmt.Sprintf("virsh net-list %s: %v", resource, err))
	}
	logrus.Infof("executing virsh net-list %s: output: %q", resource, result)

	for _, domain := range result {
		logrus.Infof("executing virsh -c %s net-destroy %s", connect, domain)
		c1 := exec.Command("mock-nss.sh", "virsh", "-c", connect, "net-destroy", domain)
		o1, err1 := c1.CombinedOutput()
		if err1 != nil {
			return false, errors.New(fmt.Sprintf("virsh net-destroy %s %v", domain, err1))
		}
		logrus.Infof("c1 output: %s", o1)

		logrus.Infof("executing virsh -c %s net-undefine %s", connect, domain)
		c2 := exec.Command("mock-nss.sh", "virsh", "-c", connect, "net-undefine", domain)
		o2, err2 := c2.CombinedOutput()
		if err2 != nil {
			return false, errors.New(fmt.Sprintf("virsh net-undefine %s %v", domain, err2))
		}
		logrus.Infof("c2 output: %s", o2)
	}

	var found = false

	result, err = virsh (connect, "list", resource)
	if (err == nil) && (len(result) >= 1) && (result[0] != "") {
		logrus.Infof("re-executing virsh list %s: output: %q", resource, result)
		found = true
	}

	result, err = virsh (connect, "pool-list", resource)
	if (err == nil) && (len(result) >= 1) && (result[0] != "") {
		logrus.Infof("re-executing virsh pool-list %s: output: %q", resource, result)
		found = true
	}

	result, err = virsh (connect, "net-list", resource)
	if (err == nil) && (len(result) >= 1) && (result[0] != "") {
		logrus.Infof("re-executing virsh net-list %s: output: %q", resource, result)
		found = true
	}

	if found {
		logrus.Debugf("libvirt_cleaner.Cleanup: ERROR: Failed to clean up the resource %s", resource)
	} else {
		logrus.Debugf("libvirt_cleaner.Cleanup: SUCCESS: Cleaned up the resource %s", resource)
	}

	return found, nil
}
