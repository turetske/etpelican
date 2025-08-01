/***************************************************************
 *
 * Copyright (C) 2024, Pelican Project, Morgridge Institute for Research
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you
 * may not use this file except in compliance with the License.  You may
 * obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 ***************************************************************/

package daemon

import (
	"context"
)

type (
	Launcher interface {
		Name() string
		Launch(ctx context.Context) (context.Context, int, error)
		KillFunc() func(pid int, sig int) error
	}

	DaemonLauncher struct {
		DaemonName string
		Args       []string
		Uid        int
		Gid        int
		ExtraEnv   []string
		InheritFds []int
		RunDir     string
	}
)

func (launcher DaemonLauncher) Name() string {
	return launcher.DaemonName
}
