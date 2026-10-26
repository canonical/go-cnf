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

package cnftest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canonical/go-cnf/cnftest"
)

// writeTopic writes one topic file into dir, so ReadAll sees it.
func writeTopic(t *testing.T, dir, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "topic.md"), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

const caseShell = "# Topic\n\n\n## Case: x\n\n```\nto-ast: \"advised-value\"\n```\n\n### cnf\n\n```\na: 1\n```\n"

func TestReadAllValidatesBlocks(t *testing.T) {
	cases := []struct {
		name    string
		ast     string
		wantErr string
	}{
		{"clean", "{\"map\": []}\n", ""},
		{"garbage after value", "{\"map\": []}\n--- PASS: TestZzz (0.00s)\n", "trailing content after the value"},
		{"not json", "not json at all\n", "block is not JSON"},
		{"two values", "{\"a\":1} {\"b\":2}\n", "trailing content after the value"},
		{"blank lines after value", "{\"map\": []}\n\n\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTopic(t, dir, caseShell+"\n### ast\n\n```\n"+tc.ast+"```\n")
			_, err := cnftest.ReadAll(dir)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("clean block: unexpected error %s", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestReadAllValidatesJSONBlocks(t *testing.T) {
	dir := t.TempDir()
	writeTopic(t, dir, "# Topic\n\n\n## Case: x\n\n```\nto-json: \"advised-text\"\n```\n\n### cnf\n\n```\na: 1\n```\n\n### json\n\n```\n{\"a\":1} trailing\n```\n")
	_, err := cnftest.ReadAll(dir)
	if err == nil || !strings.Contains(err.Error(), "json block has trailing content") {
		t.Errorf("want json trailing-content error, got %v", err)
	}
}

func TestReadAllErrorNamesCase(t *testing.T) {
	dir := t.TempDir()
	writeTopic(t, dir, caseShell+"\n### ast\n\n```\n{\"map\": []} oops\n```\n")
	_, err := cnftest.ReadAll(dir)
	if err == nil || !strings.Contains(err.Error(), "topic/x:") {
		t.Errorf("want error naming topic/case, got %v", err)
	}
}

// TestReadAllValidatesBlockReaders pins the dead-block checks:
// a block no directive reads is a case authoring bug, reported
// at read time. Each row builds a case with the given
// directives and blocks, and expects the given error.
func TestReadAllValidatesBlockReaders(t *testing.T) {
	cases := []struct {
		name    string
		directs string // the directive block's contents
		blocks  string // the ### sections after it
		wantErr string
	}{{
		name:    "cnf read by to-json",
		directs: "to-json: \"advised-text\"\n",
		blocks:  "### cnf\n\n```\na: 1\n```\n\n### json\n\n```\n{\"a\":1}\n```\n",
	}, {
		name:    "cnf read by to-json error",
		directs: "to-json: \"error\"\nerror: \"line 1: no\"\n",
		blocks:  "### cnf\n\n```\na\n```\n",
	}, {
		name:    "cnf read by from-json",
		directs: "from-json: \"advised-text\"\n",
		blocks:  "### cnf\n\n```\na: 1\n```\n\n### json\n\n```\n{\"a\":1}\n```\n",
	}, {
		name:    "cnf dead under from-json error",
		directs: "from-json: \"error\"\nerror: \"boom\"\n",
		blocks:  "### cnf\n\n```\na: 1\n```\n\n### json\n\n```\n{\"a\":1}\n```\n",
		wantErr: "cnf block without a directive that reads it",
	}, {
		name:    "json dead without value directives",
		directs: "to-ast: \"advised-value\"\n",
		blocks:  "### cnf\n\n```\na: 1\n```\n\n### json\n\n```\n{\"a\":1}\n```\n\n### ast\n\n```\n{\"value\":{\"map\":[],\"pos\":{\"l\":0,\"c\":0}}}\n```\n",
		wantErr: "json block without a directive that reads it",
	}, {
		name:    "json read by to-json",
		directs: "to-json: \"advised-text\"\n",
		blocks:  "### cnf\n\n```\na: 1\n```\n\n### json\n\n```\n{\"a\":1}\n```\n",
	}, {
		name:    "ast dead without ast directives",
		directs: "to-json: \"advised-text\"\n",
		blocks:  "### cnf\n\n```\na: 1\n```\n\n### json\n\n```\n{\"a\":1}\n```\n\n### ast\n\n```\n{\"value\":{\"map\":[],\"pos\":{\"l\":0,\"c\":0}}}\n```\n",
		wantErr: "ast block without a directive that reads it",
	}, {
		name:    "ast read by to-ast",
		directs: "to-ast: \"advised-value\"\n",
		blocks:  "### cnf\n\n```\na: 1\n```\n\n### ast\n\n```\n{\"value\":{\"map\":[],\"pos\":{\"l\":0,\"c\":0}}}\n```\n",
	}, {
		name:    "ast dead under to-ast error",
		directs: "to-ast: \"error\"\nerror: \"line 1: no\"\n",
		blocks:  "### cnf\n\n```\na\n```\n\n### ast\n\n```\n{\"value\":{\"map\":[],\"pos\":{\"l\":0,\"c\":0}}}\n```\n",
		wantErr: "ast block without a directive that reads it",
	}, {
		name:    "ast read by from-ast error",
		directs: "from-ast: \"error\"\nerror: \"boom\"\n",
		blocks:  "### ast\n\n```\n{\"value\":{\"list\":[],\"pos\":{\"l\":1,\"c\":1}},\"layout\":0}\n```\n",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTopic(t, dir, "# Topic\n\n\n## Case: x\n\n```\n"+tc.directs+"```\n\n"+tc.blocks)
			_, err := cnftest.ReadAll(dir)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("clean case: unexpected error %s", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
