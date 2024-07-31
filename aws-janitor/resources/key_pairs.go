/*
Copyright 2022 The Kubernetes Authors.

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

package resources

import (
	"context"
	"fmt"
	"time"

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// KeyPairs: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeKeyPairs

type KeyPairs struct{}

// MarkAndSweep looks at the provided set, and removes resources older than its TTL that have been previously tagged.
func (KeyPairs) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	if !opts.EnableKeyPairsClean {
		logger.Info("Disable key pairs clean")
		return nil
	}
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	resp, err := svc.DescribeKeyPairs(context.TODO(), &ec2v2.DescribeKeyPairsInput{})
	if err != nil {
		return err
	}

	for _, kp := range resp.KeyPairs {
		k := &keyPair{
			Account: opts.Account,
			Region:  opts.Region,
			ID:      *kp.KeyPairId,
		}
		tags := fromEC2Tags(kp.Tags)
		// Mark old key pairs as delete.
		if !set.Mark(opts, k, kp.CreateTime, tags) {
			continue
		}
		logger.Warningf("%s: deleting %T: %s (%s)", k.ARN(), kp, *kp.KeyPairId, tags[NameTagKey])
		if opts.DryRun {
			continue
		}

		if _, err := svc.DeleteKeyPair(context.TODO(),
			&ec2v2.DeleteKeyPairInput{KeyName: kp.KeyName, KeyPairId: kp.KeyPairId}); err != nil {
			logger.Warningf("%s: delete failed: %v", k.ARN(), err)
		}
	}

	return nil
}

func (KeyPairs) ListAll(opts Options) (*Set, error) {
	set := NewSet(0)
	if !opts.EnableKeyPairsClean {
		return set, nil
	}
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	input := &ec2v2.DescribeKeyPairsInput{}

	resp, err := svc.DescribeKeyPairs(context.TODO(), input)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't describe KeyPairs for %q in %q", opts.Account, opts.Region)
	}

	now := time.Now()
	for _, kp := range resp.KeyPairs {
		arn := keyPair{
			Account: opts.Account,
			Region:  opts.Region,
			ID:      *kp.KeyPairId,
		}.ARN()
		set.firstSeen[arn] = now
	}

	return set, nil
}

type keyPair struct {
	Account string
	Region  string
	ID      string
}

func (kp keyPair) ARN() string {
	// The ARN is synthetic using region + account + key pair ID.
	return fmt.Sprintf("arn:aws:ec2:%s:%s:keypair/%s", kp.Region, kp.Account, kp.ID)
}

func (kp keyPair) ResourceKey() string {
	return kp.ARN()
}
