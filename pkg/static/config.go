/*
Copyright 2024 The Kubernetes Authors.

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

package static

import (
	"encoding/json"
	"fmt"
)

type Config struct {
	EtcdVersion string       `json:"etcdVersion"`
	Nodes       []ConfigNode `json:"nodes"`
}

type ConfigNode struct {
	ID string   `json:"id"`
	IP []string `json:"ip"`
}

func ParseStaticConfig(config string) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal([]byte(config), c); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", config, err)
	}
	return c, nil
}