// Copyright (c) 2018 Cisco and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"context"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewOSSignalChannel() chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c,
		os.Interrupt,
		// More Linux signals here
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	return c
}

// WaitForPortAvailable waits while the port will is available. Throws exception if the context is done.
func WaitForPortAvailable(ctx context.Context, protoType, registryAddress string, idleSleep time.Duration) error {
	if idleSleep < 0 {
		return errors.New("idleSleep must be positive")
	}
	logrus.Infof("Waiting for liveness probe: %s:%s", protoType, registryAddress)
	last := time.Now()

	for {
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for: " + protoType + ":" + registryAddress)
		default:
			var d net.Dialer
			conn, err := d.DialContext(ctx, protoType, registryAddress)
			if conn != nil {
				_ = conn.Close()
			}
			if err == nil {
				return nil
			}
			if time.Since(last) > time.Minute {
				logrus.Infof("Waiting for liveness probe: %s:%s", protoType, registryAddress)
				last = time.Now()
			}
			// Sleep to not overflow network
			<-time.After(idleSleep)
		}
	}
}

// ReadEnvBool reads environment variable and treat it as bool
func ReadEnvBool(env string, value bool) (bool, error) {
	str := os.Getenv(env)
	if str == "" {
		return value, nil
	}

	return strconv.ParseBool(str)
}

// IsInsecure checks environment variable INSECURE
func IsInsecure() (bool, error) {
	insecure, err := ReadEnvBool(InsecureEnv, insecureDefault)
	if err != nil {
		return false, errors.WithMessage(err, "unable to clarify secure or insecure mode")
	}
	return insecure, nil
}
