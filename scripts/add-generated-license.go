// Copyright 2026-present Orbit Contributors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const license = `// Copyright 2026-present Orbit Contributors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
`

func main() {
	for _, name := range []string{
		"internal/wire/process_plugin.pb.go",
		"internal/wire/process_plugin_grpc.pb.go",
	} {
		if err := prepend(name); err != nil {
			fmt.Fprintln(os.Stderr, "could not license generated source")
			os.Exit(1)
		}
	}
}

func prepend(name string) error {
	content, err := os.ReadFile(filepath.Clean(name))
	if err != nil {
		return err
	}
	if strings.Contains(string(content[:min(len(content), 1024)]), "Licensed under the Apache License") {
		return nil
	}
	return os.WriteFile(name, append([]byte(license), content...), 0o644)
}
