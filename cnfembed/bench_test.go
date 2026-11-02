// Copyright 2026 Canonical Inc.
// Originally by Gustavo Niemeyer.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cnfembed_test

import (
	"os"
	"testing"

	"github.com/canonical/go-cnf/cnfembed"
)

var benchFileEnv = os.Getenv("CNF_BENCH_FILE")
var benchFileDefault = "../tmp/benchmark.cnf"
var benchFilePath = func() string {
	if benchFileEnv != "" {
		return benchFileEnv
	}
	return benchFileDefault
}()

// BenchmarkFile parses and translates the large real-world
// document named by CNF_BENCH_FILE end to end.
func BenchmarkFile(b *testing.B) {
	data, err := os.ReadFile(benchFilePath)
	if err != nil {
		if benchFileEnv == "" {
			b.Skip(benchFilePath + " not found")
		}
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cnfembed.ToJSON(data, nil); err != nil {
			b.Fatal(err)
		}
	}
}
